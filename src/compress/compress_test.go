package compress

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

var fable = []byte(`The Frog and the Crocodile
Once, there was a frog who lived in the middle of a swamp. His entire family had lived in that swamp for generations, but this particular frog decided that he had had quite enough wetness to last him a lifetime. He decided that he was going to find a dry place to live instead.

The only thing that separated him from dry land was a swampy, muddy, swiftly flowing river. But the river was home to all sorts of slippery, slittering snakes that loved nothing better than a good, plump frog for dinner, so Frog didn't dare try to swim across.

So for many days, the frog stayed put, hopping along the bank, trying to think of a way to get across.

The snakes hissed and jeered at him, daring him to come closer, but he refused. Occasionally they would slither closer, jaws open to attack, but the frog always leaped out of the way. But no matter how far upstream he searched or how far downstream, the frog wasn't able to find a way across the water.

He had felt certain that there would be a bridge, or a place where the banks came together, yet all he found was more reeds and water. After a while, even the snakes stopped teasing him and went off in search of easier prey.

The frog sighed in frustration and sat to sulk in the rushes. Suddenly, he spotted two big eyes staring at him from the water. The giant log-shaped animal opened its mouth and asked him, "What are you doing, Frog? Surely there are enough flies right there for a meal."

The frog croaked in surprise and leaped away from the crocodile. That creature could swallow him whole in a moment without thinking about it! Once he was a satisfied that he was a safe distance away, he answered. "I'm tired of living in swampy waters, and I want to travel to the other side of the river. But if I swim across, the snakes will eat me."

The crocodile harrumphed in agreement and sat, thinking, for a while. "Well, if you're afraid of the snakes, I could give you a ride across," he suggested.

"Oh no, I don't think so," Frog answered quickly. "You'd eat me on the way over, or go underwater so the snakes could get me!"

"Now why would I let the snakes get you? I think they're a terrible nuisance with all their hissing and slithering! The river would be much better off without them altogether! Anyway, if you're so worried that I might eat you, you can ride on my tail."

The frog considered his offer. He did want to get to dry ground very badly, and there didn't seem to be any other way across the river. He looked at the crocodile from his short, squat buggy eyes and wondered about the crocodile's motives. But if he rode on the tail, the croc couldn't eat him anyway. And he was right about the snakes--no self-respecting crocodile would give a meal to the snakes.

"Okay, it sounds like a good plan to me. Turn around so I can hop on your tail."

The crocodile flopped his tail into the marshy mud and let the frog climb on, then he waddled out to the river. But he couldn't stick his tail into the water as a rudder because the frog was on it -- and if he put his tail in the water, the snakes would eat the frog. They clumsily floated downstream for a ways, until the crocodile said, "Hop onto my back so I can steer straight with my tail." The frog moved, and the journey smoothed out.

From where he was sitting, the frog couldn't see much except the back of Crocodile's head. "Why don't you hop up on my head so you can see everything around us?" Crocodile invited. `)

func BenchmarkCompressLevelMinusTwo(b *testing.B) {
	for i := 0; i < b.N; i++ {
		CompressWithOption(fable, -2)
	}
}

func BenchmarkCompressLevelNine(b *testing.B) {
	for i := 0; i < b.N; i++ {
		CompressWithOption(fable, 9)
	}
}

func BenchmarkCompressLevelMinusTwoBinary(b *testing.B) {
	data := make([]byte, 1000000)
	if _, err := rand.Read(data); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		CompressWithOption(data, -2)
	}
}

func BenchmarkTransferChunkCompression(b *testing.B) {
	data := bytes.Repeat([]byte("croc-transfer-data-"), 2048)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.Run("legacy-new-writer-per-chunk", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			CompressWithOption(data, flate.HuffmanOnly)
		}
	})
	b.Run("pooled-writer", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			Compress(data)
		}
	})
	b.Run("pooled-writer-reused-output", func(b *testing.B) {
		output := make([]byte, 0, len(data))
		for i := 0; i < b.N; i++ {
			output = CompressTo(output, data)
		}
	})
}

func BenchmarkTransferChunkDecompression(b *testing.B) {
	data := bytes.Repeat([]byte("croc-transfer-data-"), 2048)
	compressed := Compress(data)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.Run("fresh-output", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := Decompress(compressed, int64(len(data))); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("reused-output", func(b *testing.B) {
		output := make([]byte, 0, len(data))
		for i := 0; i < b.N; i++ {
			var err error
			output, err = DecompressTo(output, compressed, int64(len(data)))
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

var benchmarkOutput []byte

func BenchmarkIncompressibleChunkDecision(b *testing.B) {
	data := make([]byte, 32*1024+8)
	if _, err := rand.Read(data); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.Run("always-compress", func(b *testing.B) {
		output := make([]byte, 0, len(data))
		for range b.N {
			output = CompressTo(output, data)
		}
		benchmarkOutput = output
	})
	b.Run("adaptive-raw", func(b *testing.B) {
		for range b.N {
			benchmarkOutput = data
		}
	})
}

func BenchmarkCompressLevelNineBinary(b *testing.B) {
	data := make([]byte, 1000000)
	if _, err := rand.Read(data); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		CompressWithOption(data, 9)
	}
}

func TestCompress(t *testing.T) {
	compressedB := CompressWithOption(fable, 9)
	dataRateSavings := 100 * (1.0 - float64(len(compressedB))/float64(len(fable)))
	fmt.Printf("Level 9: %2.0f%% percent space savings\n", dataRateSavings)
	assert.True(t, len(compressedB) < len(fable))
	decompressedB, err := Decompress(compressedB, int64(len(fable)))
	assert.NoError(t, err)
	assert.Equal(t, fable, decompressedB)

	compressedB = CompressWithOption(fable, -2)
	dataRateSavings = 100 * (1.0 - float64(len(compressedB))/float64(len(fable)))
	fmt.Printf("Level -2: %2.0f%% percent space savings\n", dataRateSavings)
	assert.True(t, len(compressedB) < len(fable))

	compressedB = Compress(fable)
	dataRateSavings = 100 * (1.0 - float64(len(compressedB))/float64(len(fable)))
	fmt.Printf("Level -2: %2.0f%% percent space savings\n", dataRateSavings)
	assert.True(t, len(compressedB) < len(fable))

	data := make([]byte, 4096)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	compressedB = CompressWithOption(data, -2)
	dataRateSavings = 100 * (1.0 - float64(len(compressedB))/float64(len(data)))
	fmt.Printf("random, Level -2: %2.0f%% percent space savings\n", dataRateSavings)

	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	compressedB = CompressWithOption(data, 9)
	dataRateSavings = 100 * (1.0 - float64(len(compressedB))/float64(len(data)))

	fmt.Printf("random, Level 9: %2.0f%% percent space savings\n", dataRateSavings)

}

func TestDecompressWithinLimit(t *testing.T) {
	src := []byte("bounded decompression")
	compressed := Compress(src)

	for _, limit := range []int64{int64(len(src)), int64(len(src) + 1)} {
		decompressed, err := Decompress(compressed, limit)
		assert.NoError(t, err)
		assert.Equal(t, src, decompressed)
	}
}

func TestDecompressRejectsOversizedOutput(t *testing.T) {
	src := bytes.Repeat([]byte("a"), 2*1024*1024)
	decompressed, err := Decompress(Compress(src), 1024*1024)

	assert.Nil(t, decompressed)
	assert.ErrorIs(t, err, ErrDecompressedSizeExceeded)
}

func TestDecompressRejectsMalformedInput(t *testing.T) {
	decompressed, err := Decompress([]byte("not a deflate stream"), 1024)

	assert.Nil(t, decompressed)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrDecompressedSizeExceeded)
}

func TestDecompressAllowsEmptyOutputAtZeroLimit(t *testing.T) {
	decompressed, err := Decompress(Compress(nil), 0)

	assert.NoError(t, err)
	assert.Empty(t, decompressed)
}

func TestDecompressRejectsNegativeLimit(t *testing.T) {
	decompressed, err := Decompress(nil, -1)

	assert.Nil(t, decompressed)
	assert.Error(t, err)
}
