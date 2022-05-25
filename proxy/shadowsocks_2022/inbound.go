package shadowsocks_2022

import (
	"context"

	"github.com/sagernet/sing-shadowsocks"
	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	C "github.com/sagernet/sing/common"
	B "github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/transport/internet/stat"
)

func init() {
	common.Must(common.RegisterConfig((*ServerConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewServer(ctx, config.(*ServerConfig))
	}))
}

type Inbound struct {
	networks []net.Network
	service  shadowsocks.Service
	email    string
	level    int
}

func NewServer(ctx context.Context, config *ServerConfig) (*Inbound, error) {
	networks := config.Network
	if len(networks) == 0 {
		networks = []net.Network{
			net.Network_TCP,
			net.Network_UDP,
		}
	}
	inbound := &Inbound{
		networks: networks,
		email:    config.Email,
		level:    int(config.Level),
	}
	if !C.Contains(shadowaead_2022.List, config.Method) {
		return nil, newError("unsupported method ", config.Method)
	}
	service, err := shadowaead_2022.NewServiceWithPassword(config.Method, config.Key, 500, inbound)
	if err != nil {
		return nil, newError("create service").Base(err)
	}
	inbound.service = service
	return inbound, nil
}

func (i *Inbound) Network() []net.Network {
	return i.networks
}

func (i *Inbound) Process(ctx context.Context, network net.Network, connection stat.Connection, dispatcher routing.Dispatcher) error {
	inbound := session.InboundFromContext(ctx)

	var metadata M.Metadata
	if inbound.Source.IsValid() {
		metadata.Source = M.ParseSocksaddr(inbound.Source.NetAddr())
	}

	ctx = session.ContextWithDispatcher(ctx, dispatcher)

	if network == net.Network_TCP {
		return i.service.NewConnection(ctx, connection, metadata)
	} else {
		reader := buf.NewReader(connection)
		pc := &natPacketConn{connection}
		for {
			mb, err := reader.ReadMultiBuffer()
			if err != nil {
				return err
			}
			for _, buffer := range mb {
				err = i.service.NewPacket(ctx, pc, B.As(buffer.Bytes()), metadata)
				if err != nil {
					return err
				}
			}
		}
	}
}

func (i *Inbound) NewConnection(ctx context.Context, conn net.Conn, metadata M.Metadata) error {
	inbound := session.InboundFromContext(ctx)
	inbound.User = &protocol.MemoryUser{
		Email: i.email,
		Level: uint32(i.level),
	}
	ctx = log.ContextWithAccessMessage(ctx, &log.AccessMessage{
		From:   metadata.Source,
		To:     metadata.Destination,
		Status: log.AccessAccepted,
		Email:  i.email,
	})
	newError("tunnelling request to tcp:", metadata.Destination).WriteToLog(session.ExportIDToError(ctx))
	dispatcher := session.DispatcherFromContext(ctx)
	link, err := dispatcher.Dispatch(ctx, toDestination(metadata.Destination, net.Network_TCP))
	if err != nil {
		return err
	}
	outConn := &pipeConnWrapper{
		&buf.BufferedReader{Reader: link.Reader},
		link.Writer,
		conn,
	}
	return bufio.CopyConn(ctx, conn, outConn)
}

func (i *Inbound) NewPacketConnection(ctx context.Context, conn N.PacketConn, metadata M.Metadata) error {
	inbound := session.InboundFromContext(ctx)
	inbound.User = &protocol.MemoryUser{
		Email: i.email,
		Level: uint32(i.level),
	}
	ctx = log.ContextWithAccessMessage(ctx, &log.AccessMessage{
		From:   metadata.Source,
		To:     metadata.Destination,
		Status: log.AccessAccepted,
		Email:  i.email,
	})
	newError("tunnelling request to udp:", metadata.Destination).WriteToLog(session.ExportIDToError(ctx))
	dispatcher := session.DispatcherFromContext(ctx)
	destination := toDestination(metadata.Destination, net.Network_UDP)
	link, err := dispatcher.Dispatch(ctx, destination)
	if err != nil {
		return err
	}
	outConn := &packetConnWrapper{
		Reader: link.Reader,
		Writer: link.Writer,
		Dest:   destination,
	}
	return bufio.CopyPacketConn(ctx, conn, outConn)
}

func (i *Inbound) HandleError(err error) {
	newError(err).AtWarning().WriteToLog()
}

type natPacketConn struct {
	net.Conn
}

func (c *natPacketConn) ReadPacket(buffer *B.Buffer) (addr M.Socksaddr, err error) {
	_, err = buffer.ReadFrom(c)
	return
}

func (c *natPacketConn) WritePacket(buffer *B.Buffer, addr M.Socksaddr) error {
	_, err := buffer.WriteTo(c)
	return err
}
