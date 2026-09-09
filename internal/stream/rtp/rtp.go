// Package rtp parses RTP transport: headers, H264 payload shapes, SDP and
// multicast helpers. Pure parsing plus socket setup, no session state.
package rtp

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// SDP describes the local relay (or source) as an H264 RTP session.
// sps/pps carry learned parameters so decoders join with them.
func SDP(u *url.URL, sps, pps string) string {
	s := fmt.Sprintf("v=0\no=- 0 0 IN IP4 127.0.0.1\ns=stream\nc=IN IP4 %s\nt=0 0\nm=video %s RTP/AVP 96\na=rtpmap:96 H264/90000\na=fmtp:96 packetization-mode=1", u.Hostname(), u.Port())
	if sps != "" && pps != "" {
		s += ";sprop-parameter-sets=" + sps + "," + pps
	}
	return s + "\n"
}

// RelayPort derives a stable localhost UDP port from a stream id.
func RelayPort(id string) int {
	v, _ := strconv.ParseUint(id[:4], 16, 16)
	return 30000 + int(v%20000)
}

func ifaceByIP(ip string) *net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifi := range ifaces {
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := n.IP.To4()
			if v4 != nil && v4.String() == ip {
				return &ifi
			}
		}
	}
	return nil
}

// LanIPv4 prefers 192.168/10 addresses for multicast joins.
func LanIPv4() string {
	var lan10, lan192, fallback string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := n.IP.To4()
			if ip == nil || ip[0] == 169 {
				continue
			}
			s := ip.String()
			switch {
			case ip[0] == 10 && lan10 == "":
				lan10 = s
			case ip[0] == 192 && ip[1] == 168 && lan192 == "":
				lan192 = s
			case fallback == "":
				fallback = s
			}
		}
	}
	if lan192 != "" {
		return lan192
	}
	if lan10 != "" {
		return lan10
	}
	return fallback
}

// JoinMulticast opens the multicast group socket for sniff + relay.
func JoinMulticast(u *url.URL) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(u.Hostname(), u.Port()))
	if err != nil {
		return nil, err
	}
	var ifi *net.Interface
	if lan := LanIPv4(); lan != "" {
		ifi = ifaceByIP(lan)
	}
	c, err := net.ListenMulticastUDP("udp4", ifi, addr)
	if err != nil {
		return nil, err
	}
	_ = c.SetReadBuffer(4 << 20)
	return c, nil
}

// MulticastURL parses udp/rtp URLs that point at a multicast group.
func MulticastURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	if u.Scheme != "udp" && u.Scheme != "rtp" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsMulticast() {
		return nil
	}
	return u
}

// Parse splits an RTP datagram into payload + header-extension bytes.
func Parse(b []byte) (pt int, ts uint32, payload, ext []byte, ok bool) {
	if len(b) < 12 || b[0]>>6 != 2 {
		return 0, 0, nil, nil, false
	}
	off := 12 + 4*int(b[0]&0x0f)
	if off > len(b) {
		return 0, 0, nil, nil, false
	}
	ts = uint32(b[4])<<24 | uint32(b[5])<<16 | uint32(b[6])<<8 | uint32(b[7])
	if b[0]&0x10 != 0 {
		if len(b) < off+4 {
			return 0, 0, nil, nil, false
		}
		w := (int(b[off+2]) << 8) | int(b[off+3])
		if len(b) < off+4+4*w {
			return 0, 0, nil, nil, false
		}
		ext = b[off+4 : off+4+4*w]
		off += 4 + 4*w
	}
	if off > len(b) {
		return 0, 0, nil, nil, false
	}
	payload = b[off:]
	if b[0]&0x20 != 0 && len(payload) > 0 {
		pad := int(payload[len(payload)-1])
		if pad <= len(payload) {
			payload = payload[:len(payload)-pad]
		}
	}
	return int(b[1] & 0x7f), ts, payload, ext, true
}

// NALs unpacks H264 RTP payloads: single NAL, STAP-A aggregation and
// fragmented FU-A (reassembled across calls via fu). Only SEI-carrying
// shapes are returned; video NALs yield nil.
func NALs(payload []byte, fu *[]byte) [][]byte {
	if len(payload) < 1 {
		return nil
	}
	switch payload[0] & 0x1f {
	case 6:
		return [][]byte{payload}
	case 24:
		var out [][]byte
		off := 1
		for off+2 <= len(payload) {
			n := int(payload[off])<<8 | int(payload[off+1])
			off += 2
			if n <= 0 || off+n > len(payload) {
				break
			}
			out = append(out, payload[off:off+n])
			off += n
		}
		return out
	case 28:
		if len(payload) < 2 {
			return nil
		}
		hdr := payload[1]
		orig := hdr & 0x1f
		if orig != 6 {
			return nil
		}
		if hdr&0x80 != 0 {
			*fu = append([]byte{payload[0]&0xe0 | orig}, payload[2:]...)
		} else if len(*fu) > 0 {
			*fu = append(*fu, payload[2:]...)
		}
		if hdr&0x40 != 0 && len(*fu) > 0 {
			n := *fu
			*fu = nil
			return [][]byte{n}
		}
	}
	return nil
}
