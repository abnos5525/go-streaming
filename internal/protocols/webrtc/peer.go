package webrtc

import (
	"context"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	ice "github.com/pion/ice/v4"
	pion "github.com/pion/webrtc/v4"
)

func IceServers() []pion.ICEServer { return nil }

func IceIP(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil || v4.IsLoopback() || v4[0] == 169 {
		return false
	}
	if v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31 {
		return false
	}
	return true
}

func NewPeerConnection(cap pion.RTPCodecCapability) (*pion.PeerConnection, error) {
	return newPeerConnection(cap, false, false)
}

func NewPeerConnectionForOffer(cap pion.RTPCodecCapability, offerSDP string, loopback bool) (*pion.PeerConnection, error) {
	return newPeerConnection(cap, strings.Contains(offerSDP, ".local"), loopback)
}

func WebrtcPort(id string) int {
	v, _ := strconv.ParseUint(id[4:8], 16, 16)
	return 20000 + int(v%10000)
}

func PrepareOfferSDP(sdp string, loopback bool) string {
	sdp = strings.ReplaceAll(sdp, "packetization-mode=0", "packetization-mode=1")
	if loopback {
		// Only unresolved mDNS names become loopback. A real LAN host is the
		// address Firefox is bound to; rewriting it to 127.0.0.1 fails ICE.
		sdp = mdnsCandidate.ReplaceAllString(sdp, `${1}127.0.0.1${3}`)
		return filterOfferCandidates(sdp)
	}
	return ResolveMDNS(filterOfferCandidates(sdp))
}

func filterOfferCandidates(sdp string) string {
	var out []string
	for _, line := range strings.Split(sdp, "\n") {
		if !strings.HasPrefix(line, "a=candidate:") {
			out = append(out, line)
			continue
		}
		if strings.Contains(line, ".local") {
			continue
		}
		if strings.Contains(line, " 169.254.") {
			continue
		}
		if candidateDocker172(line) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func candidateDocker172(line string) bool {
	for _, part := range strings.Fields(line) {
		if ip := net.ParseIP(part); ip != nil {
			v4 := ip.To4()
			if v4 != nil && v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31 {
				return true
			}
		}
	}
	return false
}

func ResolveMDNS(sdp string) string {
	if !strings.Contains(sdp, ".local") {
		return sdp
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	seen := map[string]string{}
	return mdnsCandidate.ReplaceAllStringFunc(sdp, func(line string) string {
		m := mdnsCandidate.FindStringSubmatch(line)
		if len(m) != 4 {
			return line
		}
		ip, ok := seen[m[2]]
		if !ok {
			ip = lookupV4(ctx, m[2])
			seen[m[2]] = ip
		}
		if ip == "" {
			return line
		}
		return m[1] + ip + m[3]
	})
}

func IceGatherWait() time.Duration {
	if strings.TrimSpace(os.Getenv("HOST_IP")) != "" {
		return 200 * time.Millisecond
	}
	return 2 * time.Second
}

func newPeerConnection(cap pion.RTPCodecCapability, firefoxMDNS, loopback bool) (*pion.PeerConnection, error) {
	se := pion.SettingEngine{}
	if firefoxMDNS {
		se.SetICEMulticastDNSMode(ice.MulticastDNSModeQueryOnly)
	}
	if ip := strings.TrimSpace(os.Getenv("HOST_IP")); ip != "" {
		se.SetNAT1To1IPs([]string{ip}, pion.ICECandidateTypeHost)
		se.SetIPFilter(func(a net.IP) bool { return a.To4() != nil && a.String() == ip })
	} else if loopback || firefoxMDNS {
		se.SetIncludeLoopbackCandidate(true)
		se.SetIPFilter(func(a net.IP) bool {
			if a.To4() != nil && a.IsLoopback() {
				return true
			}
			return IceIP(a)
		})
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

var mdnsCandidate = regexp.MustCompile(`(a=candidate:\S+ \d+ \S+ \d+ )([A-Za-z0-9-]+\.local)( \d+)`)

func lookupV4(ctx context.Context, host string) string {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if v4 := addr.IP.To4(); v4 != nil && !v4.IsLoopback() {
			return v4.String()
		}
	}
	return ""
}
