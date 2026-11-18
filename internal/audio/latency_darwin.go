package audio

import (
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const coreAudioPath = "/System/Library/Frameworks/CoreAudio.framework/CoreAudio"

// fourCC packs a Core Audio property code such as 'ltnc'.
func fourCC(s string) uint32 {
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}

var (
	selDefaultOutput = fourCC("dOut")
	selLatency       = fourCC("ltnc")
	selSafetyOffset  = fourCC("saft")
	selBufferFrames  = fourCC("fsiz")
	selStreams       = fourCC("stm#")
	selSampleRate    = fourCC("nsrt")
	scopeGlobal      = fourCC("glob")
	scopeOutput      = fourCC("outp")
)

const systemObject = 1

type propertyAddress struct {
	selector, scope, element uint32
}

var coreAudio = sync.OnceValue(func() func(object uint32, addr *propertyAddress, size *uint32, out unsafe.Pointer) int32 {
	lib, err := purego.Dlopen(coreAudioPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil
	}
	var get func(object uint32, addr *propertyAddress, qualifierSize uint32, qualifier unsafe.Pointer, size *uint32, out unsafe.Pointer) int32
	purego.RegisterLibFunc(&get, lib, "AudioObjectGetPropertyData")
	return func(object uint32, addr *propertyAddress, size *uint32, out unsafe.Pointer) int32 {
		return get(object, addr, 0, nil, size, out)
	}
})

// deviceLatency asks Core Audio how long the default output device takes to
// play a frame it is handed: its own latency, safety offset, IO buffer and the
// stream's latency. Bluetooth headphones report far more than the built-in
// speakers, which is why this is asked rather than assumed.
func deviceLatency() (time.Duration, bool) {
	get := coreAudio()
	if get == nil {
		return 0, false
	}
	var device uint32
	size := uint32(4)
	if get(systemObject, &propertyAddress{selDefaultOutput, scopeGlobal, 0}, &size, unsafe.Pointer(&device)) != 0 || device == 0 {
		return 0, false
	}
	u32 := func(selector uint32) uint32 {
		var v uint32
		size := uint32(4)
		if get(device, &propertyAddress{selector, scopeOutput, 0}, &size, unsafe.Pointer(&v)) != 0 {
			return 0
		}
		return v
	}
	var rate float64
	size = 8
	if get(device, &propertyAddress{selSampleRate, scopeGlobal, 0}, &size, unsafe.Pointer(&rate)) != 0 || rate <= 0 {
		return 0, false
	}
	frames := u32(selLatency) + u32(selSafetyOffset) + u32(selBufferFrames)

	var streams [8]uint32
	size = uint32(unsafe.Sizeof(streams))
	if get(device, &propertyAddress{selStreams, scopeOutput, 0}, &size, unsafe.Pointer(&streams[0])) == 0 && size >= 4 {
		var v uint32
		size := uint32(4)
		if get(streams[0], &propertyAddress{selLatency, scopeGlobal, 0}, &size, unsafe.Pointer(&v)) == 0 {
			frames += v
		}
	}
	return time.Duration(float64(frames) / rate * float64(time.Second)), true
}

// oto feeds an AudioQueue with four buffers of deviceBuffer/4 and refills each
// as soon as it has played, so 3.5 of them are ahead of the speaker on average.
const queueLatency = deviceBuffer * 7 / 8

var latency struct {
	sync.Mutex
	value   time.Duration
	checked time.Time
}

// outputLatency is how long audio takes from leaving oto's player to being
// heard. The device is asked again every second, so switching to headphones is
// picked up while playing.
func outputLatency() time.Duration {
	latency.Lock()
	defer latency.Unlock()
	if time.Since(latency.checked) >= time.Second {
		latency.checked = time.Now()
		d, ok := deviceLatency()
		if !ok {
			d = 20 * time.Millisecond
		}
		latency.value = queueLatency + d
	}
	return latency.value
}
