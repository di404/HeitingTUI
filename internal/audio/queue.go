package audio

import (
	"encoding/binary"
	"sync"
)

// scopeSize is how many of the most recently played mono samples are kept for
// the visualizer. Audio is heard a while after the queue hands it over (up to
// two player buffers, the device queue and the hardware, which on Bluetooth
// can be a few hundred ms), so this has to cover all of that plus the analysis
// window; when it doesn't, the visualizer runs ahead of the sound.
const scopeSize = 1 << 16 // 1.37 s

// driftSeconds is how far the queue may grow past its target before it is cut
// back, so delay cannot build up on a live stream.
const driftSeconds = 2

// pcmQueue sits between the network and the audio device. Read never blocks
// because the device's mixer calls it while holding its own locks; when there
// is nothing to play it returns silence.
type pcmQueue struct {
	mu      sync.Mutex
	data    []byte
	head    int
	target  int
	started bool
	boost   float64
	applied float64

	// scope holds the last scopeSize samples handed to the device, mixed to
	// mono and before the volume boost; scopePos is where the next one goes.
	scope    [scopeSize]float32
	scopePos int
}

func newPCMQueue(seconds int) *pcmQueue {
	q := &pcmQueue{boost: 1, applied: 1}
	q.setTarget(seconds)
	return q
}

func (q *pcmQueue) setTarget(seconds int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.target = seconds * outputBytesPerSecond
}

// setBoost sets the amplification; 1 leaves the samples untouched.
func (q *pcmQueue) setBoost(boost float64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.boost = boost
}

func (q *pcmQueue) Write(p []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.head > 0 && len(q.data)+len(p) > cap(q.data) {
		q.data = q.data[:copy(q.data, q.data[q.head:])]
		q.head = 0
	}
	q.data = append(q.data, p...)
	queued := len(q.data) - q.head
	if queued >= q.target {
		q.started = true
	}
	if queued > q.target+driftSeconds*outputBytesPerSecond {
		q.head += (queued - q.target) &^ (outputBytesPerFrame - 1)
	}
}

func (q *pcmQueue) Read(p []byte) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	size := len(p) &^ (outputBytesPerFrame - 1)
	if q.started && q.head == len(q.data) {
		q.started = false
	}
	if !q.started {
		clear(p[:size])
		q.record(p[:size])
		q.applied = q.boost
		return size, nil
	}
	n := copy(p[:size], q.data[q.head:])
	q.head += n
	q.record(p[:n])
	amplify(p[:n], q.applied, q.boost)
	q.applied = q.boost
	return n, nil
}

func (q *pcmQueue) playing() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.started
}

// record adds the frames in pcm to the scope.
func (q *pcmQueue) record(pcm []byte) {
	for i := 0; i+3 < len(pcm); i += outputBytesPerFrame {
		l := int16(binary.LittleEndian.Uint16(pcm[i:]))
		r := int16(binary.LittleEndian.Uint16(pcm[i+2:]))
		q.scope[q.scopePos] = (float32(l) + float32(r)) / 65536
		q.scopePos = (q.scopePos + 1) % scopeSize
	}
}

// samples fills dst with the samples that end delay frames before the newest
// one handed to the device, oldest first.
func (q *pcmQueue) samples(dst []float32, delay int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := min(len(dst), scopeSize)
	delay = min(max(delay, 0), scopeSize-n)
	start := ((q.scopePos-delay-n)%scopeSize + scopeSize) % scopeSize
	copied := copy(dst[:n], q.scope[start:])
	copy(dst[copied:n], q.scope[:])
}
