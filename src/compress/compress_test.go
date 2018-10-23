package compress

import (
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
	rand.Read(data)
	for i := 0; i < b.N; i++ {
		CompressWithOption(data, -2)
	}
}

func BenchmarkCompressLevelNineBinary(b *testing.B) {
	data := make([]byte, 1000000)
	rand.Read(data)
	for i := 0; i < b.N; i++ {
		CompressWithOption(data, 9)
	}
}

func TestCompress(t *testing.T) {
	compressedB := CompressWithOption(fable, 9)
	dataRateSavings := 100 * (1.0 - float64(len(compressedB))/float64(len(fable)))
	fmt.Printf("Level 9: %2.0f%% percent space savings\n", dataRateSavings)
	assert.True(t, len(compressedB) < len(fable))

	compressedB = CompressWithOption(fable, -2)
	dataRateSavings = 100 * (1.0 - float64(len(compressedB))/float64(len(fable)))
	fmt.Printf("Level -2: %2.0f%% percent space savings\n", dataRateSavings)
	assert.True(t, len(compressedB) < len(fable))
}
