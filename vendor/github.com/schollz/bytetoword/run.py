import random

words = []
with open("dictionary.txt","rb") as f:
	for line in f:
		words.append(line.split(b"(")[0].strip().lower())

words = list(set(words))
print(len(words))
word_lengths = {}
word_skips = list(b" !@#$%^&*()_+-=[]{};':\<>?,./"+b'"')
for word in words:
	if len(word) < 2:
		continue
	skip_word = False
	for w in word_skips:
		if w in word:
			skip_word = True 
			break
	if skip_word:
		continue
	word_lengths[word] = len(word)

print(len(word_lengths))
import operator
possible_words = []
for t in sorted(word_lengths.items(), key=operator.itemgetter(1)):
	possible_words.append(t[0])
	if len(possible_words) > 65536+256:
		break

random.shuffle(possible_words)
with open('words.txt','wb') as f:
	f.write((b"\n").join(possible_words))
# https://play.golang.org/p/VMtD2WfmH4D