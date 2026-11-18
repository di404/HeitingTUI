package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Minimal fragmented-MP4 reading: enough to find the AAC track in an init
// segment and pull its access units out of media segments.

var errBadMP4 = errors.New("fMP4 数据损坏")

type box struct {
	kind string
	data []byte // payload, after the header
	// start is the box's offset in the buffer it was read from.
	start int
}

// boxes splits buf into its top-level boxes. base is buf's offset in the
// enclosing buffer, so that box.start stays absolute.
func boxes(buf []byte, base int) ([]box, error) {
	var out []box
	for pos := 0; pos < len(buf); {
		if len(buf)-pos < 8 {
			return nil, errBadMP4
		}
		size := uint64(binary.BigEndian.Uint32(buf[pos:]))
		kind := string(buf[pos+4 : pos+8])
		header := uint64(8)
		switch size {
		case 0:
			size = uint64(len(buf) - pos)
		case 1:
			if len(buf)-pos < 16 {
				return nil, errBadMP4
			}
			size, header = binary.BigEndian.Uint64(buf[pos+8:]), 16
		}
		if size < header || size > uint64(len(buf)-pos) {
			return nil, errBadMP4
		}
		out = append(out, box{kind: kind, data: buf[pos+int(header) : pos+int(size)], start: base + pos})
		pos += int(size)
	}
	return out, nil
}

func child(parent []byte, kind string) []byte {
	children, err := boxes(parent, 0)
	if err != nil {
		return nil
	}
	for _, b := range children {
		if b.kind == kind {
			return b.data
		}
	}
	return nil
}

// mp4Track is the audio track described by an init segment.
type mp4Track struct {
	id          uint32
	config      []byte // AudioSpecificConfig
	defaultSize uint32 // from trex
}

func parseInit(buf []byte) (*mp4Track, error) {
	moov := child(buf, "moov")
	if moov == nil {
		return nil, errBadMP4
	}
	children, err := boxes(moov, 0)
	if err != nil {
		return nil, err
	}
	var track *mp4Track
	for _, b := range children {
		if b.kind != "trak" {
			continue
		}
		mdia := child(b.data, "mdia")
		hdlr := child(mdia, "hdlr")
		if len(hdlr) < 12 || string(hdlr[8:12]) != "soun" {
			continue
		}
		id, ok := trackID(child(b.data, "tkhd"))
		if !ok {
			return nil, errBadMP4
		}
		stsd := child(child(child(mdia, "minf"), "stbl"), "stsd")
		if len(stsd) < 8 {
			return nil, errBadMP4
		}
		entry := child(stsd[8:], "mp4a")
		if entry == nil {
			return nil, fatalError{msg: "不支持的音频格式", detail: "fmp4 audio track is not mp4a"}
		}
		config, err := mp4aConfig(entry)
		if err != nil {
			return nil, err
		}
		track = &mp4Track{id: id, config: config}
		break
	}
	if track == nil {
		return nil, errors.New("fMP4 中没有音频轨")
	}
	if trexes, err := boxes(child(moov, "mvex"), 0); err == nil {
		for _, b := range trexes {
			if b.kind == "trex" && len(b.data) >= 24 && binary.BigEndian.Uint32(b.data[4:]) == track.id {
				track.defaultSize = binary.BigEndian.Uint32(b.data[16:])
			}
		}
	}
	return track, nil
}

func trackID(tkhd []byte) (uint32, bool) {
	if len(tkhd) < 4 {
		return 0, false
	}
	offset := 12
	if tkhd[0] == 1 {
		offset = 20
	}
	if len(tkhd) < offset+4 {
		return 0, false
	}
	return binary.BigEndian.Uint32(tkhd[offset:]), true
}

// mp4aConfig reads the AudioSpecificConfig from an mp4a sample entry's esds.
func mp4aConfig(entry []byte) ([]byte, error) {
	if len(entry) < 28 {
		return nil, errBadMP4
	}
	skip := 28
	switch binary.BigEndian.Uint16(entry[8:]) { // QuickTime sound version
	case 1:
		skip += 16
	case 2:
		skip += 36
	}
	if len(entry) < skip {
		return nil, errBadMP4
	}
	esds := child(entry[skip:], "esds")
	if len(esds) < 4 {
		return nil, errBadMP4
	}
	d := esds[4:]
	tag, body, _, ok := descriptor(d)
	if !ok || tag != 3 || len(body) < 3 {
		return nil, errBadMP4
	}
	flags := body[2]
	body = body[3:]
	if flags&0x80 != 0 {
		body = trim(body, 2)
	}
	if flags&0x40 != 0 && len(body) > 0 {
		body = trim(body, 1+int(body[0]))
	}
	if flags&0x20 != 0 {
		body = trim(body, 2)
	}
	tag, body, _, ok = descriptor(body)
	if !ok || tag != 4 || len(body) < 13 {
		return nil, errBadMP4
	}
	if objectType := body[0]; objectType != 0x40 {
		return nil, fatalError{msg: "不支持的音频格式", detail: fmt.Sprintf("fmp4 object type %#x", objectType)}
	}
	tag, body, _, ok = descriptor(body[13:])
	if !ok || tag != 5 || len(body) < 2 {
		return nil, errBadMP4
	}
	return body, nil
}

func trim(b []byte, n int) []byte {
	if n > len(b) {
		return nil
	}
	return b[n:]
}

// descriptor reads one MPEG-4 descriptor: a tag, a 1–4 byte length, a body.
func descriptor(b []byte) (tag byte, body, rest []byte, ok bool) {
	if len(b) < 2 {
		return 0, nil, nil, false
	}
	tag = b[0]
	size, i := 0, 1
	for ; i < len(b) && i <= 4; i++ {
		size = size<<7 | int(b[i]&0x7f)
		if b[i]&0x80 == 0 {
			break
		}
	}
	i++
	if i > len(b) || size > len(b)-i {
		return 0, nil, nil, false
	}
	return tag, b[i : i+size], b[i+size:], true
}

// mp4Samples returns the track's access units in a media segment, in order.
func mp4Samples(buf []byte, track *mp4Track) ([][]byte, error) {
	top, err := boxes(buf, 0)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, moof := range top {
		if moof.kind != "moof" {
			continue
		}
		trafs, err := boxes(moof.data, 0)
		if err != nil {
			return nil, err
		}
		for _, traf := range trafs {
			if traf.kind != "traf" {
				continue
			}
			out, err = trafSamples(out, buf, moof.start, traf.data, track)
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func trafSamples(out [][]byte, buf []byte, moofStart int, traf []byte, track *mp4Track) ([][]byte, error) {
	tfhd := child(traf, "tfhd")
	if len(tfhd) < 8 {
		return nil, errBadMP4
	}
	if binary.BigEndian.Uint32(tfhd[4:]) != track.id {
		return out, nil
	}
	flags := binary.BigEndian.Uint32(tfhd) & 0xffffff
	base := uint64(moofStart)
	defaultSize := track.defaultSize
	p := tfhd[8:]
	if flags&0x01 != 0 {
		if len(p) < 8 {
			return nil, errBadMP4
		}
		base, p = binary.BigEndian.Uint64(p), p[8:]
	}
	for _, f := range []uint32{0x02, 0x08} {
		if flags&f != 0 {
			p = trim(p, 4)
		}
	}
	if flags&0x10 != 0 {
		if len(p) < 4 {
			return nil, errBadMP4
		}
		defaultSize = binary.BigEndian.Uint32(p)
	}

	runs, err := boxes(traf, 0)
	if err != nil {
		return nil, err
	}
	next := base
	for _, run := range runs {
		if run.kind != "trun" {
			continue
		}
		t := run.data
		if len(t) < 8 {
			return nil, errBadMP4
		}
		flags := binary.BigEndian.Uint32(t) & 0xffffff
		count := binary.BigEndian.Uint32(t[4:])
		t = t[8:]
		offset := next
		if flags&0x01 != 0 {
			if len(t) < 4 {
				return nil, errBadMP4
			}
			offset = uint64(int64(base) + int64(int32(binary.BigEndian.Uint32(t))))
			t = t[4:]
		}
		if flags&0x04 != 0 {
			t = trim(t, 4)
		}
		var fields int
		for _, f := range []uint32{0x100, 0x200, 0x400, 0x800} {
			if flags&f != 0 {
				fields++
			}
		}
		if uint64(len(t)) < uint64(count)*uint64(fields)*4 {
			return nil, errBadMP4
		}
		for range count {
			size := defaultSize
			if flags&0x100 != 0 {
				t = t[4:]
			}
			if flags&0x200 != 0 {
				size, t = binary.BigEndian.Uint32(t), t[4:]
			}
			if flags&0x400 != 0 {
				t = t[4:]
			}
			if flags&0x800 != 0 {
				t = t[4:]
			}
			if offset+uint64(size) > uint64(len(buf)) {
				return nil, errBadMP4
			}
			if size > 0 {
				out = append(out, buf[offset:offset+uint64(size)])
			}
			offset += uint64(size)
		}
		next = offset
	}
	return out, nil
}
