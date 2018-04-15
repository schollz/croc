<p align="center">
<img
    src="https://user-images.githubusercontent.com/6550035/31846899-2b8a7034-b5cf-11e7-9643-afe552226c59.png"
    width="100%" border="0" alt="croc">
<br>
<a href="https://travis-ci.org/schollz/croc"><img src="https://travis-ci.org/schollz/croc.svg?branch=master" alt="Build Status"></a>
<a href="https://github.com/schollz/croc/releases/latest"><img src="https://img.shields.io/badge/version-0.6.0-brightgreen.svg?style=flat-square" alt="Version"></a>
<a href="https://goreportcard.com/report/github.com/schollz/croc"><img src="https://goreportcard.com/badge/github.com/schollz/croc" alt="Go Report Card"></a>
</p>

<p align="center">Secure transfer of stuff from one side of the internet to the other.</p>

This is more or less (but mostly *less*) a Golang port of [@warner's](https://github.com/warner) [*magic-wormhole*](https://github.com/warner/magic-wormhole) which allows you to directly transfer files and folders between computers. I decided to make this because I wanted to send my friend Jessie a file using *magic-wormhole* and when I told Jessie how to install the dependencies she made this face: :sob:. So, nominally, *croc* does the same thing (encrypted file transfer directly between computers) without dependencies so you can just double-click on your computer, even if you use Windows.

**Don't we have enough open-source peer-to-peer file-transfer utilities?**

[There](https://github.com/cowbell/sharedrop) [are](https://github.com/webtorrent/instant.io) [great](https://github.com/kern/filepizza) [tools](https://github.com/warner/magic-wormhole) [that](https://github.com/zerotier/toss) [already](https://github.com/ipfs/go-ipfs) [do](https://github.com/zerotier/toss) [this](https://github.com/nils-werner/zget). But, no we don't, because after review, [I found it was useful to make a new one](https://schollz.github.io/sending-a-file/).

# Example

_These two gifs should run in sync if you force-reload (Ctl+F5)_

**Sender:**

![send](https://user-images.githubusercontent.com/6550035/32415611-9bfa858a-c1f9-11e7-9573-52c1edc52e5f.gif)

**Receiver:**

![receive](https://user-images.githubusercontent.com/6550035/32415609-9ba49dc8-c1f9-11e7-9c86-ea0baae730fe.gif)


**Sender:**

```
$ croc -send croc.exe
Sending 4.4 MB file named 'croc.exe'
Code is: 4-cement-galaxy-alpha

Sending (->[1]63982)..
  89% |███████████████████████████████████     | [12s:1s]
File sent (2.6 MB/s)
```

**Receiver:**

```
$ croc
Enter receive code: 4-cement-galaxy-alpha
Receiving file (4.4 MB) into: croc.exe
ok? (y/n): y

Receiving (<-[1]63975)..
  97% |██████████████████████████████████████  | [13s:0s]
Received file written to croc.exe (2.6 MB/s)
```

Note, by default, you don't need any arguments for receiving! This makes it possible for you to just double click the executable to run (nice for those of us that aren't computer wizards).


# Install

[Download the latest release for your system](https://github.com/schollz/croc/releases/latest).

Or, you can [install Go](https://golang.org/dl/) and build from source with `go get github.com/schollz/croc`.



# How does it work?

*croc* is similar to [magic-wormhole](https://github.com/warner/magic-wormhole#design) in spirit and design. Like *magic-wormhole*, *croc* generates a code phrase for you to share with your friend which allows secure end-to-end transfering of files and folders through a intermediary relay that  connects the TCP ports between the two computers.

In *croc*, code phrase is 16 random bits that are [menemonic encoded](http://web.archive.org/web/20101031205747/http://www.tothink.com/mnemonic/) plus a prepended integer to specify number of threads. This code phrase is hashed using sha256 and sent to a relay which maps that key to that connection. When the relay finds a matching key for both the receiver and the sender (i.e. they both have the same code phrase), then the sender transmits the encrypted metadata to the receiver through the relay. Then the receiver decrypts and reviews the metadata (file name, size), and chooses whether to consent to the transfer.

After the receiver consents to the transfer, the sender transmits encrypted data through the relay. The relay setups up [Go channels](https://golang.org/doc/effective_go.html?h=chan#channels) for each connection which pipes all the data incoming from that sender's connection out to the receiver's connection. After the transmission the channels are destroyed and all the connection and meta data information is wiped from the relay server. The encrypted file data never is stored on the relay.

**Encryption**

Encryption uses PBKDF2 (see [RFC2898](http://www.ietf.org/rfc/rfc2898.txt)) where the code phrase shared between the sender and receiver is used as the passphrase. For each of the two encrypted data blocks (metadata stored on relay server, and file data transmitted), a random 8-byte salt is used and a IV is generated according to [NIST Recommendation for Block ciphers, Section 8.2](http://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf).


**Decryption**

On the receiver's computer, each piece of received encrypted data is written to a separate file. These files are concatenated and then decrypted. The hash of the decrypted file is then checked against the hash transmitted from the sender (part of the meta data block).

## Run your own relay

*croc* relies on a TCP relay to staple the parallel incoming and outgoing connections. The relay temporarily stores connection information and the encrypted meta information. The default uses a public relay at, `cowyo.com`, which has no guarantees except that I guarantee to turn if off as soon as it gets abused ([click here to check the current status of the public relay](https://stats.uptimerobot.com/lOwJYIgRm)).

I recommend you run your own relay, it is very easy. On your server, `your-server.com`, just run

```
$ croc -relay
```

Now, when you use *croc* to send and receive you should add `-server your-server.com` to use your relay server.

_Note:_ If you are behind a firewall, make sure to open up TCP ports 27001-27009.

# Contribute

I am awed by all the [great contributions](#acknowledgements) made! If you feel like contributing, in any way, by all means you can send an Issue, a PR, ask a question, or tweet me ([@yakczar](http://ctt.ec/Rq054)).

# License

MIT

# Acknowledgements

Thanks...

- ...[@warner](https://github.com/warner) for the [idea](https://github.com/warner/magic-wormhole).
- ...[@tscholl2](https://github.com/tscholl2) for the [encryption gists](https://gist.github.com/tscholl2/dc7dc15dc132ea70a98e8542fefffa28).
- ...[@skorokithakis](https://github.com/skorokithakis) for [code on proxying two connections](https://www.stavros.io/posts/proxying-two-connections-go/).
- ...for making pull requests [@Girbons](https://github.com/Girbons), [@techtide](https://github.com/techtide), [@heymatthew](https://github.com/heymatthew), [@Lunsford94](https://github.com/Lunsford94), [@lummie](https://github.com/lummie), [@jesuiscamille](https://github.com/jesuiscamille), [@threefjord](https://github.com/threefjord), [@marcossegovia](https://github.com/marcossegovia), [@csleong98](https://github.com/csleong98), [@afotescu](https://github.com/afotescu), [@callmefever](https://github.com/callmefever), [@El-JojA](https://github.com/El-JojA), [@anatolyyyyyy](https://github.com/anatolyyyyyy), [@goggle](https://github.com/goggle), [@smileboywtu](https://github.com/smileboywtu)!
