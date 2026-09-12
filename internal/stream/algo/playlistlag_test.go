package algo

import "testing"

func TestPlaylistLagMS(t *testing.T) {
	durs, files := ParseEXTINF([]byte("#EXTM3U\n#EXTINF:2.0,\na.ts\n#EXTINF:2.0,\nb.ts\n#EXTINF:2.0,\nc.ts\n#EXTINF:1.5,\nd.ts\n"))
	if len(durs) != 4 || len(files) != 4 || files[3] != "d.ts" {
		t.Fatalf("parse %v %v", durs, files)
	}
	if got := PlaylistLagMS(durs); got != 3500 {
		t.Fatalf("want 3500, got %d", got)
	}
	if got := PlaylistLagMS(durs[:1]); got != 2000 {
		t.Fatalf("startup 1 seg: want 2000, got %d", got)
	}
	if got := PlaylistLagMS(nil); got != 0 {
		t.Fatalf("empty: want 0, got %d", got)
	}
	if got := PlaylistLagMS(durs[:2]); got != 4000 {
		t.Fatalf("startup 2 segs: want 4000, got %d", got)
	}
}
