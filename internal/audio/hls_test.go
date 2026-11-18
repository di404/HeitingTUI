package audio

import (
	"context"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aac "github.com/tphakala/go-aac"
)

func mp4Box(kind string, parts ...[]byte) []byte {
	size := 8
	for _, p := range parts {
		size += len(p)
	}
	out := binary.BigEndian.AppendUint32(nil, uint32(size))
	out = append(out, kind...)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func u32(vs ...uint32) []byte {
	var out []byte
	for _, v := range vs {
		out = binary.BigEndian.AppendUint32(out, v)
	}
	return out
}

// fmp4Init describes a video track 1 and an AAC track 2.
func fmp4Init(config []byte) []byte {
	esds := append([]byte{0, 0, 0, 0, 3, byte(3 + 2 + 13 + 2 + len(config)), 0, 2, 0}, 4, byte(13+2+len(config)), 0x40, 0x15)
	esds = append(esds, make([]byte, 11)...)
	esds = append(append(esds, 5, byte(len(config))), config...)
	mp4a := mp4Box("mp4a", make([]byte, 28), mp4Box("esds", esds))
	trak := func(id uint32, handler string, entry []byte) []byte {
		return mp4Box("trak",
			mp4Box("tkhd", u32(0, 0, 0, id)),
			mp4Box("mdia",
				mp4Box("hdlr", u32(0, 0), []byte(handler)),
				mp4Box("minf", mp4Box("stbl", mp4Box("stsd", u32(0, 1), entry)))))
	}
	return mp4Box("moov",
		trak(1, "vide", mp4Box("avc1", make([]byte, 78))),
		trak(2, "soun", mp4a),
		mp4Box("mvex", mp4Box("trex", u32(0, 2, 1, 1024, 0, 0))))
}

// fmp4Segment interleaves a video sample with the audio units.
func fmp4Segment(units [][]byte) []byte {
	video := []byte{9, 9, 9}
	traf := func(id uint32, offset int, samples [][]byte) []byte {
		run := u32(0x201, uint32(len(samples)), uint32(offset))
		for _, s := range samples {
			run = append(run, u32(uint32(len(s)))...)
		}
		return mp4Box("traf", mp4Box("tfhd", u32(0x20000, id)), mp4Box("trun", run))
	}
	build := func(audioOffset, videoOffset int) []byte {
		return mp4Box("moof", mp4Box("mfhd", u32(0, 1)), traf(1, videoOffset, [][]byte{video}), traf(2, audioOffset, units))
	}
	moofSize := len(build(0, 0))
	moof := build(moofSize+8+len(video), moofSize+8)
	payload := append([]byte{}, video...)
	for _, u := range units {
		payload = append(payload, u...)
	}
	return append(moof, mp4Box("mdat", payload)...)
}

func TestHLSPlaysTheAudioTrackOfFMP4Segments(t *testing.T) {
	encoder, err := aac.NewEncoder(aac.EncoderConfig{SampleRate: outputRate, Channels: 2, Bitrate: 128000})
	if err != nil {
		t.Fatal(err)
	}
	samples := make([]float32, aac.FrameSize)
	var segments [][]byte
	for range 4 {
		var units [][]byte
		for len(units) < outputRate/4/aac.FrameSize {
			for i := range samples {
				samples[i] = float32(0.5 * math.Sin(float64(i)))
			}
			unit, err := encoder.EncodeFrame(nil, [][]float32{samples, samples})
			if err != nil {
				t.Fatal(err)
			}
			if len(unit) > 0 {
				units = append(units, unit)
			}
		}
		segments = append(segments, fmp4Segment(units))
	}
	init := fmp4Init(encoder.AudioSpecificConfig())

	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:40\n#EXT-X-MAP:URI=\"h1.m4s\"\n"
	for i := range segments {
		playlist += "#EXTINF:0.25,\n" + string(rune('a'+i)) + ".m4s\n"
	}
	playlist += "#EXT-X-ENDLIST\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/live/")
		switch {
		case name == "index.m3u8":
			w.Write([]byte(playlist))
		case name == "h1.m4s":
			w.Write(init)
		case len(name) == 5 && name[0] >= 'a' && int(name[0]-'a') < len(segments):
			w.Write(segments[name[0]-'a'])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	src, err := openHLS(context.Background(), server.Client(), server.URL+"/live/index.m3u8?sign=x", Source{})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	var out []byte
	if err := decodeStream(src, func(p []byte) { out = append(out, p...) }); err != errStreamEnded {
		t.Fatalf("got %v", err)
	}
	// Playback starts hlsLiveEdge segments from the end.
	if seconds := float64(len(out)) / outputBytesPerSecond; seconds < 0.6 || seconds > 0.9 {
		t.Fatalf("decoded %.2fs", seconds)
	}
	if peak(out[len(out)/2:]) < 5000 {
		t.Fatal("silence")
	}
}

func TestParsePlaylistFollowsVariant(t *testing.T) {
	base, _ := http.NewRequest(http.MethodGet, "https://cdn.example.com/a/master.m3u8?x=1", nil)
	pl, err := parsePlaylist([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nlow/index.m3u8\n"), base.URL)
	if err != nil || pl.variant != "https://cdn.example.com/a/low/index.m3u8" {
		t.Fatalf("got %+v %v", pl, err)
	}
}
