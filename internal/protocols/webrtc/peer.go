package webrtc

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	pion "github.com/pion/webrtc/v4"
)

// IceServers empty: LAN/VPN viewers reach HOST_IP host candidates. Public
// STUN (e.g. Google) is often blocked and stalls browser gathering.
func IceServers() []pion.ICEServer { return nil }

// IceIP filters host candidates. Docker / loopback must not be advertised.
func IceIP(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil || v4.IsLoopback() || v4[0] == 169 {
		return false
	}
	// Docker / compose bridges. Chrome picking these is ICE-connected black.
	if v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31 {
		return false
	}
	return true
}

// NewPeerConnection uses HOST_IP as the sole host candidate address when set
// (e.g. docker --network host -e HOST_IP=…). Registers only the stream
// profile (cameras here are Main avc1.4d0029). Extra 42e01f aliases make Chrome
// pick Baseline, ICE still connects (WEBRTC LIVE), and the decoder stays black.
func NewPeerConnection(cap pion.RTPCodecCapability) (*pion.PeerConnection, error) {
	se := pion.SettingEngine{}
	if ip := strings.TrimSpace(os.Getenv("HOST_IP")); ip != "" {
		se.SetNAT1To1IPs([]string{ip}, pion.ICECandidateTypeHost)
		se.SetIPFilter(func(a net.IP) bool { return a.To4() != nil && a.String() == ip })
	} else {
		se.SetIPFilter(IceIP)
	}
	se.SetNetworkTypes([]pion.NetworkType{pion.NetworkTypeUDP4})
	m := &pion.MediaEngine{}
	if cap.MimeType != "" {
		if err := m.RegisterCodec(pion.RTPCodecParameters{
			RTPCodecCapability: cap,
			PayloadType:        96,
		}, pion.RTPCodecTypeVideo); err != nil {
			return nil, err
		}
	}
	api := pion.NewAPI(pion.WithSettingEngine(se), pion.WithMediaEngine(m))
	return api.NewPeerConnection(pion.Configuration{ICEServers: IceServers()})
}

// WebrtcPort avoids relayPort's 30000-49999 range and decorrelates by
// hashing other id bytes.
func WebrtcPort(id string) int {
	v, _ := strconv.ParseUint(id[4:8], 16, 16)
	return 20000 + int(v%10000)
}

// IceGatherWait is soft-ICE gather budget: HOST_IP one-candidate fast path,
// else 2s for multi-NIC host enumeration (mediamtx-style, no trickle).
func IceGatherWait() time.Duration {
	if strings.TrimSpace(os.Getenv("HOST_IP")) != "" {
		return 200 * time.Millisecond
	}
	return 2 * time.Second
}
