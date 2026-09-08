package stream

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"
	"time"
)

func rtpSDP(u *url.URL) string {
	return fmt.Sprintf("v=0\no=- 0 0 IN IP4 127.0.0.1\ns=stream\nc=IN IP4 %s\nt=0 0\nm=video %s RTP/AVP 96\na=rtpmap:96 H264/90000\na=fmtp:96 packetization-mode=1\n", u.Hostname(), u.Port())
}

func relayPort(id string) int {
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

func joinMulticast(u *url.URL) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(u.Hostname(), u.Port()))
	if err != nil {
		return nil, err
	}
	var ifi *net.Interface
	if lan := lanIPv4(); lan != "" {
		ifi = ifaceByIP(lan)
	}
	c, err := net.ListenMulticastUDP("udp4", ifi, addr)
	if err != nil {
		return nil, err
	}
	_ = c.SetReadBuffer(4 << 20)
	return c, nil
}

func rtpPacket(b []byte) (pt int, ts uint32, payload []byte, ok bool) {
	if len(b) < 12 || b[0]>>6 != 2 {
		return 0, 0, nil, false
	}
	ts = uint32(b[4])<<24 | uint32(b[5])<<16 | uint32(b[6])<<8 | uint32(b[7])
	off := 12 + 4*int(b[0]&0x0f)
	if b[0]&0x10 != 0 {
		if len(b) < off+4 {
			return 0, 0, nil, false
		}
		off += 4 + 4*((int(b[off+2])<<8)|int(b[off+3]))
	}
	if off > len(b) {
		return 0, 0, nil, false
	}
	payload = b[off:]
	if b[0]&0x20 != 0 && len(payload) > 0 {
		pad := int(payload[len(payload)-1])
		if pad <= len(payload) {
			payload = payload[:len(payload)-pad]
		}
	}
	return int(b[1] & 0x7f), ts, payload, true
}

func h264NALs(payload []byte, fu *[]byte) [][]byte {
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

func (s *session) sniff(my int, mc *net.UDPConn, relay int) {
	defer mc.Close()
	dst, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.0.0.1:%d", relay))
	if err != nil {
		s.set(err.Error())
		return
	}
	out, err := net.DialUDP("udp4", nil, dst)
	if err != nil {
		s.set(err.Error())
		log.Printf("hls %s relay: %v", s.id, err)
		return
	}
	defer out.Close()
	buf := make([]byte, 65535)
	var fu []byte
	for {
		if !s.alive(my) {
			return
		}
		_ = mc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := mc.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		pkt := buf[:n]
		pt, ts, payload, ok := rtpPacket(pkt)
		if !ok {
			if f := parseAI(pkt); f != nil {
				s.setFrame(f)
			}
			continue
		}
		s.rtpTS.Store(ts)
		if f := parseAI(payload); f != nil {
			s.setFrame(f)
		}
		if pt != 96 {
			continue
		}
		for _, nal := range h264NALs(payload, &fu) {
			if f := nalAI(nal); f != nil {
				s.setFrame(f)
			}
		}
		_, _ = out.Write(pkt)
	}
}
