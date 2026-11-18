package audio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// hlsLiveEdge is how many segments behind the newest one playback starts,
	// as hls.js does: the newest segment may still be filling on other nodes.
	hlsLiveEdge    = 3
	maxHLSFetch    = 32 << 20
	minPlaylistGap = 500 * time.Millisecond
)

type hlsSegment struct {
	seq  uint64
	uri  string
	init string
}

type hlsPlaylist struct {
	target   time.Duration
	segments []hlsSegment
	variant  string
	ended    bool
}

func parsePlaylist(body []byte, base *url.URL) (*hlsPlaylist, error) {
	if !bytes.HasPrefix(bytes.TrimLeft(body, "\ufeff \r\n"), []byte("#EXTM3U")) {
		return nil, errors.New("不是 HLS 播放列表")
	}
	pl := &hlsPlaylist{target: time.Second}
	var seq uint64
	var init string
	variant := false
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		tag, value, _ := strings.Cut(line, ":")
		switch {
		case line == "":
		case tag == "#EXT-X-MEDIA-SEQUENCE":
			seq, _ = strconv.ParseUint(value, 10, 64)
		case tag == "#EXT-X-TARGETDURATION":
			if s, err := strconv.ParseFloat(value, 64); err == nil && s > 0 {
				pl.target = time.Duration(s * float64(time.Second))
			}
		case tag == "#EXT-X-MAP":
			if uri := attribute(value, "URI"); uri != "" {
				init = resolve(base, uri)
			}
		case tag == "#EXT-X-STREAM-INF":
			variant = true
		case tag == "#EXT-X-ENDLIST":
			pl.ended = true
		case strings.HasPrefix(line, "#"):
		case variant:
			if pl.variant == "" {
				pl.variant = resolve(base, line)
			}
		default:
			pl.segments = append(pl.segments, hlsSegment{seq: seq, uri: resolve(base, line), init: init})
			seq++
		}
	}
	return pl, scanner.Err()
}

func attribute(list, name string) string {
	for _, part := range strings.Split(list, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key == name {
			return strings.Trim(value, `"`)
		}
	}
	return ""
}

func resolve(base *url.URL, ref string) string {
	u, err := base.Parse(ref)
	if err != nil {
		return ref
	}
	return u.String()
}

// hlsSource plays the audio track of a live fMP4 HLS stream. Like flvSource,
// Read serves length-prefixed AAC access units and reports io.EOF when the
// AudioSpecificConfig changes.
type hlsSource struct {
	ctx    context.Context
	cancel context.CancelFunc
	client *http.Client
	src    Source
	url    string

	target  time.Duration
	fetched time.Time // last playlist fetch
	fresh   time.Time // last time the playlist offered a new segment
	started bool
	next    uint64
	queue   []hlsSegment

	initURI string
	track   *mp4Track
	config  []byte
	pending []byte
	units   [][]byte
	frame   []byte
}

// openHLS loads the playlist once, so a dead address fails here.
func openHLS(parent context.Context, client *http.Client, playlist string, src Source) (*hlsSource, error) {
	ctx, cancel := context.WithCancel(parent)
	s := &hlsSource{ctx: ctx, cancel: cancel, client: client, src: src, url: playlist, target: time.Second}
	if err := s.refresh(); err != nil {
		cancel()
		return nil, err
	}
	return s, nil
}

func (s *hlsSource) Close() error {
	s.cancel()
	return nil
}

func (s *hlsSource) fetch(rawURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(s.ctx, stallTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.src.UserAgent)
	req.Header.Set("Referer", s.src.Referer)
	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && s.ctx.Err() == nil {
			return nil, errStalled
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHLSFetch))
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) && s.ctx.Err() == nil {
		return nil, errStalled
	}
	return body, err
}

// refresh reloads the playlist and queues the segments not played yet.
func (s *hlsSource) refresh() error {
	base, err := url.Parse(s.url)
	if err != nil {
		return err
	}
	body, err := s.fetch(s.url)
	if err != nil {
		return err
	}
	s.fetched = time.Now()
	pl, err := parsePlaylist(body, base)
	if err != nil {
		return err
	}
	if pl.variant != "" && len(pl.segments) == 0 {
		s.url = pl.variant
		return s.refresh()
	}
	s.target = pl.target
	segments := pl.segments
	if !s.started {
		if len(segments) == 0 {
			return errors.New("HLS 播放列表为空")
		}
		segments = segments[max(0, len(segments)-hlsLiveEdge):]
		s.next = segments[0].seq
		s.started = true
		s.fresh = s.fetched
	}
	added := false
	for _, seg := range segments {
		if seg.seq >= s.next {
			s.queue = append(s.queue, seg)
			s.next = seg.seq + 1
			added = true
		}
	}
	switch {
	case added:
		s.fresh = s.fetched
	case pl.ended:
		return errStreamEnded
	case s.fetched.Sub(s.fresh) > stallTimeout:
		return errStalled
	}
	return nil
}

func (s *hlsSource) nextSegment() (hlsSegment, error) {
	for len(s.queue) == 0 {
		wait := max(s.target/2, minPlaylistGap) - time.Since(s.fetched)
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-s.ctx.Done():
				return hlsSegment{}, s.ctx.Err()
			}
		}
		if err := s.refresh(); err != nil {
			return hlsSegment{}, err
		}
	}
	seg := s.queue[0]
	s.queue = s.queue[1:]
	return seg, nil
}

// load fetches the next segment's audio, and its init segment when that
// changes.
func (s *hlsSource) load() error {
	seg, err := s.nextSegment()
	if err != nil {
		return err
	}
	if seg.init == "" {
		return fatalError{msg: "不支持的音频格式", detail: "HLS stream is not fMP4"}
	}
	if seg.init != s.initURI {
		body, err := s.fetch(seg.init)
		if err != nil {
			return err
		}
		track, err := parseInit(body)
		if err != nil {
			return err
		}
		s.initURI, s.track = seg.init, track
		if s.config == nil {
			s.config = track.config
		} else if !bytes.Equal(track.config, s.config) {
			s.pending = track.config
		}
	}
	body, err := s.fetch(seg.uri)
	if err != nil {
		return err
	}
	units, err := mp4Samples(body, s.track)
	if err != nil {
		return err
	}
	s.units = append(s.units, units...)
	return nil
}

func (s *hlsSource) nextConfig() ([]byte, error) {
	if s.pending != nil {
		s.config, s.pending = s.pending, nil
		return s.config, nil
	}
	for s.config == nil {
		if err := s.load(); err != nil {
			return nil, err
		}
	}
	return s.config, nil
}

func (s *hlsSource) Read(b []byte) (int, error) {
	for len(s.frame) == 0 {
		if s.pending != nil {
			return 0, io.EOF
		}
		if len(s.units) == 0 {
			if err := s.load(); err != nil {
				return 0, err
			}
			continue
		}
		unit := s.units[0]
		s.units = s.units[1:]
		if len(unit) > 0xffff {
			continue
		}
		s.frame = append(s.frame[:0], byte(len(unit)>>8), byte(len(unit)))
		s.frame = append(s.frame, unit...)
	}
	n := copy(b, s.frame)
	s.frame = s.frame[n:]
	return n, nil
}
