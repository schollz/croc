Mnemonicode 
===========

Mnemonicode is a method for encoding binary data into a sequence
of words which can be spoken over the phone, for example, and converted
back to data on the other side.

[![GoDoc](https://godoc.org/bitbucket.org/dchapes/mnemonicode?status.png)](https://godoc.org/bitbucket.org/dchapes/mnemonicode)

Online package documentation is available via
[https://godoc.org/bitbucket.org/dchapes/mnemonicode](https://godoc.org/bitbucket.org/dchapes/mnemonicode).

To install the package:

		go get bitbucket.org/dchapes/mnemonicode

or the command line programs:

		go get bitbucket.org/dchapes/mnemonicode/cmd/...

or `go build` any Go code that imports it:

		import "bitbucket.org/dchapes/mnemonicode"

For more information see
<https://github.com/singpolyma/mnemonicode>
or
<http://web.archive.org/web/20101031205747/http://www.tothink.com/mnemonic/>

From the README there:

There are some other somewhat similar systems that seem less satisfactory:

- OTP was designed for easy typing, and for minimizing length, but as
  a consequence the word list contains words that are similar ("AD"
  and "ADD") that are poor for dictating over the phone

- PGPfone has optimized "maximum phonetic distance" between words,
  which resolves the above problem but has some other drawbacks:

    - Low efficiency, as it encodes a little less than 1 bit per
    character;

    - Word quality issues, as some words are somewhat obscure to
    non-native speakers of English, or are awkward to use or type.

Mnemonic tries to do better by being more selective about its word
list.  Its criteria are thus:

Mandatory Criteria:

 - The wordlist contains 1626 words.

 - All words are between 4 and 7 letters long.

 - No word in the list is a prefix of another word (e.g. visit,
   visitor).

 - Five letter prefixes of words are sufficient to be unique. 

Less Strict Criteria:

  - The words should be usable by people all over the world. The list
    is far from perfect in that respect. It is heavily biased towards
    western culture and English in particular. The international
    vocabulary is simply not big enough. One can argue that even words
    like "hotel" or "radio" are not truly international. You will find
    many English words in the list but I have tried to limit them to
    words that are part of a beginner's vocabulary or words that have
    close relatives in other european languages. In some cases a word
    has a different meaning in another language or is pronounced very
    differently but for the purpose of the encoding it is still ok - I
    assume that when the encoding is used for spoken communication
    both sides speak the same language.

  - The words should have more than one syllable. This makes them
    easier to recognize when spoken, especially over a phone
    line. Again, you will find many exceptions. For one syllable words
    I have tried to use words with 3 or more consonants or words with
    diphthongs, making for a longer and more distinct
    pronounciation. As a result of this requirement the average word
    length has increased. I do not consider this to be a problem since
    my goal in limiting the word length was not to reduce the average
    length of encoded data but to limit the maximum length to fit in
    fixed-size fields or a terminal line width.

  - No two words on the list should sound too much alike. Soundalikes
    such as "sweet" and "suite" are ruled out. One of the two is
    chosen and the other should be accepted by the decoder's
    soundalike matching code or using explicit aliases for some words.

  - No offensive words. The rule was to avoid words that I would not
    like to be printed on my business card. I have extended this to
    words that by themselves are not offensive but are too likely to
    create combinations that someone may find embarrassing or
    offensive. This includes words dealing with religion such as
    "church" or "jewish" and some words with negative meanings like
    "problem" or "fiasco". I am sure that a creative mind (or a random
    number generator) can find plenty of embarrasing or offensive word
    combinations using only words in the list but I have tried to
    avoid the more obvious ones. One of my tools for this was simply a
    generator of random word combinations - the problematic ones stick
    out like a sore thumb.

  - Avoid words with tricky spelling or pronounciation. Even if the
    receiver of the message can probably spell the word close enough
    for the soundalike matcher to recognize it correctly I prefer
    avoiding such words. I believe this will help users feel more
    comfortable using the system, increase the level of confidence and
    decrease the overall error rate. Most words in the list can be
    spelled more or less correctly from hearing, even without knowing
    the word.

  - The word should feel right for the job. I know, this one is very
    subjective but some words would meet all the criteria and still
    not feel right for the purpose of mnemonic encoding. The word
    should feel like one of the words in the radio phonetic alphabets
    (alpha, bravo, charlie, delta etc).
