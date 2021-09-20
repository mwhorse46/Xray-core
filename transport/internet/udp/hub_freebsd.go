//go:build freebsd
// +build freebsd

package udp

import (
	"bytes"
	"encoding/gob"
	"io"

	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"
)

// RetrieveOriginalDest from stored laddr, caddr
func RetrieveOriginalDest(oob []byte) net.Destination {
	dec := gob.NewDecoder(bytes.NewBuffer(oob))
	var la, ra net.UDPAddr
	dec.Decode(&la)
	dec.Decode(&ra)
	ip, port, err := internet.OriginalDst(&la, &ra)
	if err != nil {
		return net.Destination{}
	}
	return net.UDPDestination(net.IPAddress(ip), net.Port(port))
}

// ReadUDPMsg stores laddr, caddr for later use
func ReadUDPMsg(conn *net.UDPConn, payload []byte, oob []byte) (int, int, int, *net.UDPAddr, error) {
	nBytes, addr, err := conn.ReadFromUDP(payload)
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	enc.Encode(conn.LocalAddr().(*net.UDPAddr))
	enc.Encode(addr)
	var reader io.Reader = &buf
	noob, _ := reader.Read(oob)
	return nBytes, noob, 0, addr, err
}
