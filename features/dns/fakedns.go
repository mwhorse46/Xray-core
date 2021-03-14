package dns

import (
	gonet "net"

	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/features"
)

type FakeDNSEngine interface {
	features.Feature
	GetFakeIPForDomain(domain string) []net.Address
	GetDomainFromFakeDNS(ip net.Address) string
	GetFakeIPRange() *gonet.IPNet
}

var FakeIPPool = "198.18.0.0/16"
