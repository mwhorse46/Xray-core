package conf

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/golang/protobuf/proto"

	"github.com/xtls/xray-core/common/platform/filesystem"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/domainsocket"
	httpheader "github.com/xtls/xray-core/transport/internet/headers/http"
	"github.com/xtls/xray-core/transport/internet/http"
	"github.com/xtls/xray-core/transport/internet/kcp"
	"github.com/xtls/xray-core/transport/internet/quic"
	"github.com/xtls/xray-core/transport/internet/tcp"
	"github.com/xtls/xray-core/transport/internet/tls"
	"github.com/xtls/xray-core/transport/internet/websocket"
	"github.com/xtls/xray-core/transport/internet/xtls"
)

var (
	kcpHeaderLoader = NewJSONConfigLoader(ConfigCreatorCache{
		"none":         func() interface{} { return new(NoOpAuthenticator) },
		"srtp":         func() interface{} { return new(SRTPAuthenticator) },
		"utp":          func() interface{} { return new(UTPAuthenticator) },
		"wechat-video": func() interface{} { return new(WechatVideoAuthenticator) },
		"dtls":         func() interface{} { return new(DTLSAuthenticator) },
		"wireguard":    func() interface{} { return new(WireguardAuthenticator) },
	}, "type", "")

	tcpHeaderLoader = NewJSONConfigLoader(ConfigCreatorCache{
		"none": func() interface{} { return new(NoOpConnectionAuthenticator) },
		"http": func() interface{} { return new(Authenticator) },
	}, "type", "")
)

type KCPConfig struct {
	Mtu             *uint32         `json:"mtu"`
	Tti             *uint32         `json:"tti"`
	UpCap           *uint32         `json:"uplinkCapacity"`
	DownCap         *uint32         `json:"downlinkCapacity"`
	Congestion      *bool           `json:"congestion"`
	ReadBufferSize  *uint32         `json:"readBufferSize"`
	WriteBufferSize *uint32         `json:"writeBufferSize"`
	HeaderConfig    json.RawMessage `json:"header"`
	Seed            *string         `json:"seed"`
}

// Build implements Buildable.
func (c *KCPConfig) Build() (proto.Message, error) {
	config := new(kcp.Config)

	if c.Mtu != nil {
		mtu := *c.Mtu
		if mtu < 576 || mtu > 1460 {
			return nil, newError("invalid mKCP MTU size: ", mtu).AtError()
		}
		config.Mtu = &kcp.MTU{Value: mtu}
	}
	if c.Tti != nil {
		tti := *c.Tti
		if tti < 10 || tti > 100 {
			return nil, newError("invalid mKCP TTI: ", tti).AtError()
		}
		config.Tti = &kcp.TTI{Value: tti}
	}
	if c.UpCap != nil {
		config.UplinkCapacity = &kcp.UplinkCapacity{Value: *c.UpCap}
	}
	if c.DownCap != nil {
		config.DownlinkCapacity = &kcp.DownlinkCapacity{Value: *c.DownCap}
	}
	if c.Congestion != nil {
		config.Congestion = *c.Congestion
	}
	if c.ReadBufferSize != nil {
		size := *c.ReadBufferSize
		if size > 0 {
			config.ReadBuffer = &kcp.ReadBuffer{Size: size * 1024 * 1024}
		} else {
			config.ReadBuffer = &kcp.ReadBuffer{Size: 512 * 1024}
		}
	}
	if c.WriteBufferSize != nil {
		size := *c.WriteBufferSize
		if size > 0 {
			config.WriteBuffer = &kcp.WriteBuffer{Size: size * 1024 * 1024}
		} else {
			config.WriteBuffer = &kcp.WriteBuffer{Size: 512 * 1024}
		}
	}
	if len(c.HeaderConfig) > 0 {
		headerConfig, _, err := kcpHeaderLoader.Load(c.HeaderConfig)
		if err != nil {
			return nil, newError("invalid mKCP header config.").Base(err).AtError()
		}
		ts, err := headerConfig.(Buildable).Build()
		if err != nil {
			return nil, newError("invalid mKCP header config").Base(err).AtError()
		}
		config.HeaderConfig = serial.ToTypedMessage(ts)
	}

	if c.Seed != nil {
		config.Seed = &kcp.EncryptionSeed{Seed: *c.Seed}
	}

	return config, nil
}

type TCPConfig struct {
	HeaderConfig        json.RawMessage `json:"header"`
	AcceptProxyProtocol bool            `json:"acceptProxyProtocol"`
}

// Build implements Buildable.
func (c *TCPConfig) Build() (proto.Message, error) {
	config := new(tcp.Config)
	if len(c.HeaderConfig) > 0 {
		headerConfig, _, err := tcpHeaderLoader.Load(c.HeaderConfig)
		if err != nil {
			return nil, newError("invalid TCP header config").Base(err).AtError()
		}
		ts, err := headerConfig.(Buildable).Build()
		if err != nil {
			return nil, newError("invalid TCP header config").Base(err).AtError()
		}
		config.HeaderSettings = serial.ToTypedMessage(ts)
	}
	if c.AcceptProxyProtocol {
		config.AcceptProxyProtocol = c.AcceptProxyProtocol
	}
	return config, nil
}

type WebSocketConfig struct {
	Path                string            `json:"path"`
	Headers             map[string]string `json:"headers"`
	AcceptProxyProtocol bool              `json:"acceptProxyProtocol"`
}

// Build implements Buildable.
func (c *WebSocketConfig) Build() (proto.Message, error) {
	path := c.Path
	header := make([]*websocket.Header, 0, 32)
	for key, value := range c.Headers {
		header = append(header, &websocket.Header{
			Key:   key,
			Value: value,
		})
	}
	var ed uint32
	if u, err := url.Parse(path); err == nil {
		if q := u.Query(); q.Get("ed") != "" {
			Ed, _ := strconv.Atoi(q.Get("ed"))
			ed = uint32(Ed)
			q.Del("ed")
			u.RawQuery = q.Encode()
			path = u.String()
		}
	}
	config := &websocket.Config{
		Path:   path,
		Header: header,
		Ed:     ed,
	}
	if c.AcceptProxyProtocol {
		config.AcceptProxyProtocol = c.AcceptProxyProtocol
	}
	return config, nil
}

type HTTPConfig struct {
	Host               *StringList            `json:"host"`
	Path               string                 `json:"path"`
	ReadIdleTimeout    int32                  `json:"read_idle_timeout"`
	HealthCheckTimeout int32                  `json:"health_check_timeout"`
	Method             string                 `json:"method"`
	Headers            map[string]*StringList `json:"headers"`
}

// Build implements Buildable.
func (c *HTTPConfig) Build() (proto.Message, error) {
	if c.ReadIdleTimeout <= 0 {
		c.ReadIdleTimeout = 0
	}
	if c.HealthCheckTimeout <= 0 {
		c.HealthCheckTimeout = 0
	}
	config := &http.Config{
		Path:               c.Path,
		IdleTimeout:        c.ReadIdleTimeout,
		HealthCheckTimeout: c.HealthCheckTimeout,
	}
	if c.Host != nil {
		config.Host = []string(*c.Host)
	}
	if c.Method != "" {
		config.Method = c.Method
	}
	if len(c.Headers) > 0 {
		config.Header = make([]*httpheader.Header, 0, len(c.Headers))
		headerNames := sortMapKeys(c.Headers)
		for _, key := range headerNames {
			value := c.Headers[key]
			if value == nil {
				return nil, newError("empty HTTP header value: " + key).AtError()
			}
			config.Header = append(config.Header, &httpheader.Header{
				Name:  key,
				Value: append([]string(nil), (*value)...),
			})
		}
	}
	return config, nil
}

type QUICConfig struct {
	Header   json.RawMessage `json:"header"`
	Security string          `json:"security"`
	Key      string          `json:"key"`
}

// Build implements Buildable.
func (c *QUICConfig) Build() (proto.Message, error) {
	config := &quic.Config{
		Key: c.Key,
	}

	if len(c.Header) > 0 {
		headerConfig, _, err := kcpHeaderLoader.Load(c.Header)
		if err != nil {
			return nil, newError("invalid QUIC header config.").Base(err).AtError()
		}
		ts, err := headerConfig.(Buildable).Build()
		if err != nil {
			return nil, newError("invalid QUIC header config").Base(err).AtError()
		}
		config.Header = serial.ToTypedMessage(ts)
	}

	var st protocol.SecurityType
	switch strings.ToLower(c.Security) {
	case "aes-128-gcm":
		st = protocol.SecurityType_AES128_GCM
	case "chacha20-poly1305":
		st = protocol.SecurityType_CHACHA20_POLY1305
	default:
		st = protocol.SecurityType_NONE
	}

	config.Security = &protocol.SecurityConfig{
		Type: st,
	}

	return config, nil
}

type DomainSocketConfig struct {
	Path     string `json:"path"`
	Abstract bool   `json:"abstract"`
	Padding  bool   `json:"padding"`
}

// Build implements Buildable.
func (c *DomainSocketConfig) Build() (proto.Message, error) {
	return &domainsocket.Config{
		Path:     c.Path,
		Abstract: c.Abstract,
		Padding:  c.Padding,
	}, nil
}

func readFileOrString(f string, s []string) ([]byte, error) {
	if len(f) > 0 {
		return filesystem.ReadFile(f)
	}
	if len(s) > 0 {
		return []byte(strings.Join(s, "\n")), nil
	}
	return nil, newError("both file and bytes are empty.")
}

type TLSCertConfig struct {
	CertFile       string   `json:"certificateFile"`
	CertStr        []string `json:"certificate"`
	KeyFile        string   `json:"keyFile"`
	KeyStr         []string `json:"key"`
	Usage          string   `json:"usage"`
	OcspStapling   uint64   `json:"ocspStapling"`
	OneTimeLoading bool     `json:"oneTimeLoading"`
}

// Build implements Buildable.
func (c *TLSCertConfig) Build() (*tls.Certificate, error) {
	certificate := new(tls.Certificate)

	cert, err := readFileOrString(c.CertFile, c.CertStr)
	if err != nil {
		return nil, newError("failed to parse certificate").Base(err)
	}
	certificate.Certificate = cert
	certificate.CertificatePath = c.CertFile

	if len(c.KeyFile) > 0 || len(c.KeyStr) > 0 {
		key, err := readFileOrString(c.KeyFile, c.KeyStr)
		if err != nil {
			return nil, newError("failed to parse key").Base(err)
		}
		certificate.Key = key
		certificate.KeyPath = c.KeyFile
	}

	switch strings.ToLower(c.Usage) {
	case "encipherment":
		certificate.Usage = tls.Certificate_ENCIPHERMENT
	case "verify":
		certificate.Usage = tls.Certificate_AUTHORITY_VERIFY
	case "issue":
		certificate.Usage = tls.Certificate_AUTHORITY_ISSUE
	default:
		certificate.Usage = tls.Certificate_ENCIPHERMENT
	}
	if certificate.KeyPath == "" && certificate.CertificatePath == "" {
		certificate.OneTimeLoading = true
	} else {
		certificate.OneTimeLoading = c.OneTimeLoading
	}
	certificate.OcspStapling = c.OcspStapling

	return certificate, nil
}

type TLSConfig struct {
	Insecure                         bool             `json:"allowInsecure"`
	Certs                            []*TLSCertConfig `json:"certificates"`
	ServerName                       string           `json:"serverName"`
	ALPN                             *StringList      `json:"alpn"`
	EnableSessionResumption          bool             `json:"enableSessionResumption"`
	DisableSystemRoot                bool             `json:"disableSystemRoot"`
	MinVersion                       string           `json:"minVersion"`
	MaxVersion                       string           `json:"maxVersion"`
	CipherSuites                     string           `json:"cipherSuites"`
	PreferServerCipherSuites         bool             `json:"preferServerCipherSuites"`
	Fingerprint                      string           `json:"fingerprint"`
	RejectUnknownSNI                 bool             `json:"rejectUnknownSni"`
	PinnedPeerCertificateChainSha256 *[]string        `json:"pinnedPeerCertificateChainSha256"`
}

// Build implements Buildable.
func (c *TLSConfig) Build() (proto.Message, error) {
	config := new(tls.Config)
	config.Certificate = make([]*tls.Certificate, len(c.Certs))
	for idx, certConf := range c.Certs {
		cert, err := certConf.Build()
		if err != nil {
			return nil, err
		}
		config.Certificate[idx] = cert
	}
	serverName := c.ServerName
	config.AllowInsecure = c.Insecure
	if len(c.ServerName) > 0 {
		config.ServerName = serverName
	}
	if c.ALPN != nil && len(*c.ALPN) > 0 {
		config.NextProtocol = []string(*c.ALPN)
	}
	config.EnableSessionResumption = c.EnableSessionResumption
	config.DisableSystemRoot = c.DisableSystemRoot
	config.MinVersion = c.MinVersion
	config.MaxVersion = c.MaxVersion
	config.CipherSuites = c.CipherSuites
	config.PreferServerCipherSuites = c.PreferServerCipherSuites
	config.Fingerprint = strings.ToLower(c.Fingerprint)
	config.RejectUnknownSni = c.RejectUnknownSNI

	if c.PinnedPeerCertificateChainSha256 != nil {
		config.PinnedPeerCertificateChainSha256 = [][]byte{}
		for _, v := range *c.PinnedPeerCertificateChainSha256 {
			hashValue, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return nil, err
			}
			config.PinnedPeerCertificateChainSha256 = append(config.PinnedPeerCertificateChainSha256, hashValue)
		}
	}

	return config, nil
}

type XTLSCertConfig struct {
	CertFile       string   `json:"certificateFile"`
	CertStr        []string `json:"certificate"`
	KeyFile        string   `json:"keyFile"`
	KeyStr         []string `json:"key"`
	Usage          string   `json:"usage"`
	OcspStapling   uint64   `json:"ocspStapling"`
	OneTimeLoading bool     `json:"oneTimeLoading"`
}

// Build implements Buildable.
func (c *XTLSCertConfig) Build() (*xtls.Certificate, error) {
	certificate := new(xtls.Certificate)
	cert, err := readFileOrString(c.CertFile, c.CertStr)
	if err != nil {
		return nil, newError("failed to parse certificate").Base(err)
	}
	certificate.Certificate = cert
	certificate.CertificatePath = c.CertFile

	if len(c.KeyFile) > 0 || len(c.KeyStr) > 0 {
		key, err := readFileOrString(c.KeyFile, c.KeyStr)
		if err != nil {
			return nil, newError("failed to parse key").Base(err)
		}
		certificate.Key = key
		certificate.KeyPath = c.KeyFile
	}

	switch strings.ToLower(c.Usage) {
	case "encipherment":
		certificate.Usage = xtls.Certificate_ENCIPHERMENT
	case "verify":
		certificate.Usage = xtls.Certificate_AUTHORITY_VERIFY
	case "issue":
		certificate.Usage = xtls.Certificate_AUTHORITY_ISSUE
	default:
		certificate.Usage = xtls.Certificate_ENCIPHERMENT
	}
	if certificate.KeyPath == "" && certificate.CertificatePath == "" {
		certificate.OneTimeLoading = true
	} else {
		certificate.OneTimeLoading = c.OneTimeLoading
	}
	certificate.OcspStapling = c.OcspStapling

	return certificate, nil
}

type XTLSConfig struct {
	Insecure                         bool              `json:"allowInsecure"`
	Certs                            []*XTLSCertConfig `json:"certificates"`
	ServerName                       string            `json:"serverName"`
	ALPN                             *StringList       `json:"alpn"`
	EnableSessionResumption          bool              `json:"enableSessionResumption"`
	DisableSystemRoot                bool              `json:"disableSystemRoot"`
	MinVersion                       string            `json:"minVersion"`
	MaxVersion                       string            `json:"maxVersion"`
	CipherSuites                     string            `json:"cipherSuites"`
	PreferServerCipherSuites         bool              `json:"preferServerCipherSuites"`
	RejectUnknownSNI                 bool              `json:"rejectUnknownSni"`
	PinnedPeerCertificateChainSha256 *[]string         `json:"pinnedPeerCertificateChainSha256"`
}

// Build implements Buildable.
func (c *XTLSConfig) Build() (proto.Message, error) {
	config := new(xtls.Config)
	config.Certificate = make([]*xtls.Certificate, len(c.Certs))
	for idx, certConf := range c.Certs {
		cert, err := certConf.Build()
		if err != nil {
			return nil, err
		}
		config.Certificate[idx] = cert
	}
	serverName := c.ServerName
	config.AllowInsecure = c.Insecure
	if len(c.ServerName) > 0 {
		config.ServerName = serverName
	}
	if c.ALPN != nil && len(*c.ALPN) > 0 {
		config.NextProtocol = []string(*c.ALPN)
	}
	config.EnableSessionResumption = c.EnableSessionResumption
	config.DisableSystemRoot = c.DisableSystemRoot
	config.MinVersion = c.MinVersion
	config.MaxVersion = c.MaxVersion
	config.CipherSuites = c.CipherSuites
	config.PreferServerCipherSuites = c.PreferServerCipherSuites
	config.RejectUnknownSni = c.RejectUnknownSNI

	if c.PinnedPeerCertificateChainSha256 != nil {
		config.PinnedPeerCertificateChainSha256 = [][]byte{}
		for _, v := range *c.PinnedPeerCertificateChainSha256 {
			hashValue, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return nil, err
			}
			config.PinnedPeerCertificateChainSha256 = append(config.PinnedPeerCertificateChainSha256, hashValue)
		}
	}

	return config, nil
}

type TransportProtocol string

// Build implements Buildable.
func (p TransportProtocol) Build() (string, error) {
	switch strings.ToLower(string(p)) {
	case "tcp":
		return "tcp", nil
	case "kcp", "mkcp":
		return "mkcp", nil
	case "ws", "websocket":
		return "websocket", nil
	case "h2", "http":
		return "http", nil
	case "ds", "domainsocket":
		return "domainsocket", nil
	case "quic":
		return "quic", nil
	case "grpc", "gun":
		return "grpc", nil
	default:
		return "", newError("Config: unknown transport protocol: ", p)
	}
}

type SocketConfig struct {
	Mark                 int32       `json:"mark"`
	TFO                  interface{} `json:"tcpFastOpen"`
	TProxy               string      `json:"tproxy"`
	AcceptProxyProtocol  bool        `json:"acceptProxyProtocol"`
	DomainStrategy       string      `json:"domainStrategy"`
	DialerProxy          string      `json:"dialerProxy"`
	TCPKeepAliveInterval int32       `json:"tcpKeepAliveInterval"`
}

// Build implements Buildable.
func (c *SocketConfig) Build() (*internet.SocketConfig, error) {
	tfo := int32(0) // don't invoke setsockopt() for TFO
	if c.TFO != nil {
		switch v := c.TFO.(type) {
		case bool:
			if v {
				tfo = 256
			} else {
				tfo = -1 // TFO need to be disabled
			}
		case float64:
			tfo = int32(math.Min(v, math.MaxInt32))
		default:
			return nil, newError("tcpFastOpen: only boolean and integer value is acceptable")
		}
	}
	var tproxy internet.SocketConfig_TProxyMode
	switch strings.ToLower(c.TProxy) {
	case "tproxy":
		tproxy = internet.SocketConfig_TProxy
	case "redirect":
		tproxy = internet.SocketConfig_Redirect
	default:
		tproxy = internet.SocketConfig_Off
	}

	dStrategy := internet.DomainStrategy_AS_IS
	switch strings.ToLower(c.DomainStrategy) {
	case "useip", "use_ip":
		dStrategy = internet.DomainStrategy_USE_IP
	case "useip4", "useipv4", "use_ipv4", "use_ip_v4", "use_ip4":
		dStrategy = internet.DomainStrategy_USE_IP4
	case "useip6", "useipv6", "use_ipv6", "use_ip_v6", "use_ip6":
		dStrategy = internet.DomainStrategy_USE_IP6
	}

	return &internet.SocketConfig{
		Mark:                 c.Mark,
		Tfo:                  tfo,
		Tproxy:               tproxy,
		DomainStrategy:       dStrategy,
		AcceptProxyProtocol:  c.AcceptProxyProtocol,
		DialerProxy:          c.DialerProxy,
		TcpKeepAliveInterval: c.TCPKeepAliveInterval,
	}, nil
}

type StreamConfig struct {
	Network        *TransportProtocol  `json:"network"`
	Security       string              `json:"security"`
	TLSSettings    *TLSConfig          `json:"tlsSettings"`
	XTLSSettings   *XTLSConfig         `json:"xtlsSettings"`
	TCPSettings    *TCPConfig          `json:"tcpSettings"`
	KCPSettings    *KCPConfig          `json:"kcpSettings"`
	WSSettings     *WebSocketConfig    `json:"wsSettings"`
	HTTPSettings   *HTTPConfig         `json:"httpSettings"`
	DSSettings     *DomainSocketConfig `json:"dsSettings"`
	QUICSettings   *QUICConfig         `json:"quicSettings"`
	SocketSettings *SocketConfig       `json:"sockopt"`
	GRPCConfig     *GRPCConfig         `json:"grpcSettings"`
	GUNConfig      *GRPCConfig         `json:"gunSettings"`
}

// Build implements Buildable.
func (c *StreamConfig) Build() (*internet.StreamConfig, error) {
	config := &internet.StreamConfig{
		ProtocolName: "tcp",
	}
	if c.Network != nil {
		protocol, err := c.Network.Build()
		if err != nil {
			return nil, err
		}
		config.ProtocolName = protocol
	}
	if strings.EqualFold(c.Security, "tls") {
		tlsSettings := c.TLSSettings
		if tlsSettings == nil {
			if c.XTLSSettings != nil {
				return nil, newError(`TLS: Please use "tlsSettings" instead of "xtlsSettings".`)
			}
			tlsSettings = &TLSConfig{}
		}
		ts, err := tlsSettings.Build()
		if err != nil {
			return nil, newError("Failed to build TLS config.").Base(err)
		}
		tm := serial.ToTypedMessage(ts)
		config.SecuritySettings = append(config.SecuritySettings, tm)
		config.SecurityType = tm.Type
	}
	if strings.EqualFold(c.Security, "xtls") {
		if config.ProtocolName != "tcp" && config.ProtocolName != "mkcp" && config.ProtocolName != "domainsocket" {
			return nil, newError("XTLS only supports TCP, mKCP and DomainSocket for now.")
		}
		xtlsSettings := c.XTLSSettings
		if xtlsSettings == nil {
			if c.TLSSettings != nil {
				return nil, newError(`XTLS: Please use "xtlsSettings" instead of "tlsSettings".`)
			}
			xtlsSettings = &XTLSConfig{}
		}
		ts, err := xtlsSettings.Build()
		if err != nil {
			return nil, newError("Failed to build XTLS config.").Base(err)
		}
		tm := serial.ToTypedMessage(ts)
		config.SecuritySettings = append(config.SecuritySettings, tm)
		config.SecurityType = tm.Type
	}
	if c.TCPSettings != nil {
		ts, err := c.TCPSettings.Build()
		if err != nil {
			return nil, newError("Failed to build TCP config.").Base(err)
		}
		config.TransportSettings = append(config.TransportSettings, &internet.TransportConfig{
			ProtocolName: "tcp",
			Settings:     serial.ToTypedMessage(ts),
		})
	}
	if c.KCPSettings != nil {
		ts, err := c.KCPSettings.Build()
		if err != nil {
			return nil, newError("Failed to build mKCP config.").Base(err)
		}
		config.TransportSettings = append(config.TransportSettings, &internet.TransportConfig{
			ProtocolName: "mkcp",
			Settings:     serial.ToTypedMessage(ts),
		})
	}
	if c.WSSettings != nil {
		ts, err := c.WSSettings.Build()
		if err != nil {
			return nil, newError("Failed to build WebSocket config.").Base(err)
		}
		config.TransportSettings = append(config.TransportSettings, &internet.TransportConfig{
			ProtocolName: "websocket",
			Settings:     serial.ToTypedMessage(ts),
		})
	}
	if c.HTTPSettings != nil {
		ts, err := c.HTTPSettings.Build()
		if err != nil {
			return nil, newError("Failed to build HTTP config.").Base(err)
		}
		config.TransportSettings = append(config.TransportSettings, &internet.TransportConfig{
			ProtocolName: "http",
			Settings:     serial.ToTypedMessage(ts),
		})
	}
	if c.DSSettings != nil {
		ds, err := c.DSSettings.Build()
		if err != nil {
			return nil, newError("Failed to build DomainSocket config.").Base(err)
		}
		config.TransportSettings = append(config.TransportSettings, &internet.TransportConfig{
			ProtocolName: "domainsocket",
			Settings:     serial.ToTypedMessage(ds),
		})
	}
	if c.QUICSettings != nil {
		qs, err := c.QUICSettings.Build()
		if err != nil {
			return nil, newError("Failed to build QUIC config").Base(err)
		}
		config.TransportSettings = append(config.TransportSettings, &internet.TransportConfig{
			ProtocolName: "quic",
			Settings:     serial.ToTypedMessage(qs),
		})
	}
	if c.GRPCConfig == nil {
		c.GRPCConfig = c.GUNConfig
	}
	if c.GRPCConfig != nil {
		gs, err := c.GRPCConfig.Build()
		if err != nil {
			return nil, newError("Failed to build gRPC config.").Base(err)
		}
		config.TransportSettings = append(config.TransportSettings, &internet.TransportConfig{
			ProtocolName: "grpc",
			Settings:     serial.ToTypedMessage(gs),
		})
	}
	if c.SocketSettings != nil {
		ss, err := c.SocketSettings.Build()
		if err != nil {
			return nil, newError("Failed to build sockopt").Base(err)
		}
		config.SocketSettings = ss
	}
	return config, nil
}

type ProxyConfig struct {
	Tag string `json:"tag"`

	// TransportLayerProxy: For compatibility.
	TransportLayerProxy bool `json:"transportLayer"`
}

// Build implements Buildable.
func (v *ProxyConfig) Build() (*internet.ProxyConfig, error) {
	if v.Tag == "" {
		return nil, newError("Proxy tag is not set.")
	}
	return &internet.ProxyConfig{
		Tag:                 v.Tag,
		TransportLayerProxy: v.TransportLayerProxy,
	}, nil
}
