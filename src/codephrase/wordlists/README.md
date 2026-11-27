# Vendored code word lists

## Google English list used for generation

`google-english-no-swears.txt` combines the US English no-swears medium and
short lists from [first20hours/google-10000-english](https://github.com/first20hours/google-10000-english),
pinned to commit `d0736d492489198e4f9d650c7ab4143bc14c1e9e`.
Both Go and the browser load this same snapshot; no network access is needed
to generate or parse codes.

The inputs, in concatenation order, are:

1. [`google-10000-english-usa-no-swears-medium.txt`](https://raw.githubusercontent.com/first20hours/google-10000-english/d0736d492489198e4f9d650c7ab4143bc14c1e9e/google-10000-english-usa-no-swears-medium.txt)
   - Original SHA-256: `07d682123e8b94b5097357a165c1bd806533a1af8ddf503f30f499ef27dfc043`
   - 5,459 entries retained.
2. [`google-10000-english-usa-no-swears-short.txt`](https://raw.githubusercontent.com/first20hours/google-10000-english/d0736d492489198e4f9d650c7ab4143bc14c1e9e/google-10000-english-usa-no-swears-short.txt)
   - Original SHA-256: `1030d0323755a31fda0533887940053f9f352833cefea881a3dfb6e0899b8a76`
   - 1,764 entries retained; 420 entries of one or two letters removed.

To reproduce the snapshot, concatenate the inputs in that order, strip
surrounding whitespace from each line, convert to lowercase, retain only
entries matching `^[a-z]{3,}$`, and deduplicate while keeping first occurrence
order. Write one word per line using LF endings, including a final newline.
This excludes hyphens, punctuation, digits, and entries shorter than three
letters. The resulting 7,223 entries have lengths of three through eight letters.
Selection is uniform over the entire combined list, without frequency weighting.

- Vendored SHA-256: `b407eaf72beeecb8d51a234492c132688fdd562128d9bf2405199e1c50aef2c3`

The upstream attribution and usage notice is preserved verbatim in
`google-LICENSE.md` and in the repository's `THIRD_PARTY_NOTICES.md`, which
is distributed with release artifacts. The EFF license below applies only
to the legacy EFF list.

## Legacy EFF Short Wordlist #1 snapshot

This list is retained for parsing earlier codes and SSH invitations, including
the hyphenated entry `yo-yo`. It is no longer used to generate new codes.

`eff-short-wordlist-1.txt` contains the word column from the Electronic
Frontier Foundation's [Short Wordlist #1](https://www.eff.org/files/2016/09/08/eff_short_wordlist_1.txt).
The original four-dice labels were removed; the words and their order are
otherwise unchanged.

- Original EFF file SHA-256: `8f5ca830b8bffb6fe39c9736c024a00a6a6411adb3f83a9be8bfeeb6e067ae69`
- Vendored word-only file SHA-256: `36ecca49e4fa20ca84b176c32f2e9c82f98f446585190e75f9879a95c08247bf`

The word list is by the Electronic Frontier Foundation and is licensed under
the Creative Commons Attribution 4.0 International License. See `LICENSE.txt`
for the license text.
