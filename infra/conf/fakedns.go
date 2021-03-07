package conf

import (
	"github.com/golang/protobuf/proto"
	"github.com/xtls/xray-core/app/dns/fakedns"
)

type FakeDNSConfig struct {
	IPPool  string `json:"ipPool"`
	LruSize int64  `json:"poolSize"`
}

func (f FakeDNSConfig) Build() (proto.Message, error) {
	return &fakedns.FakeDnsPool{
		IpPool:  f.IPPool,
		LruSize: f.LruSize,
	}, nil
}

type FakeDNSPostProcessingStage struct{}

func (FakeDNSPostProcessingStage) Process(conf *Config) error {
	var fakeDNSInUse bool

	if conf.DNSConfig != nil {
		for _, v := range conf.DNSConfig.Servers {
			if v.Address.Family().IsDomain() {
				if v.Address.Domain() == "fakedns" {
					fakeDNSInUse = true
				}
			}
		}
	}

	if fakeDNSInUse {
		if conf.FakeDNS == nil {
			// Add a Fake DNS Config if there is none
			conf.FakeDNS = &FakeDNSConfig{
				IPPool:  "240.0.0.0/8",
				LruSize: 65535,
			}
		}
		found := false
		// Check if there is a Outbound with necessary sniffer on
		var inbounds []InboundDetourConfig

		if len(conf.InboundConfigs) > 0 {
			inbounds = append(inbounds, conf.InboundConfigs...)
		}
		for _, v := range inbounds {
			if v.SniffingConfig != nil && v.SniffingConfig.Enabled && v.SniffingConfig.DestOverride != nil {
				for _, dov := range *v.SniffingConfig.DestOverride {
					if dov == "fakedns" {
						found = true
					}
				}
			}
		}
		if !found {
			newError("Defined Fake DNS but haven't enabled fake dns sniffing at any inbound.").AtWarning().WriteToLog()
		}
	}

	return nil
}
