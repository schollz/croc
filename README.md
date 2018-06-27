<p align="center">
<img
    src="https://user-images.githubusercontent.com/6550035/31846899-2b8a7034-b5cf-11e7-9643-afe552226c59.png"
    width="100%" border="0" alt="croc">
<br>
<a href="https://github.com/schollz/croc/releases/latest"><img src="https://img.shields.io/badge/version-β2.0.0-brightgreen.svg?style=flat-square" alt="Version"></a>
<a href="https://saythanks.io/to/schollz"><img src="https://img.shields.io/badge/Say%20Thanks-!-yellow.svg?style=flat-square" alt="Go Report Card"></a>
</p>


<p align="center">Easily and securely transfer stuff from one computer to another.</p>

*croc* allows any two computers to directly and securely transfer files and folders. When sending a file, *croc* generates a random code phrase which must be shared with the recipient so they can receive the file. The code phrase encrypts all data and metadata and also serves to authorize the connection between the two computers in a intermediary relay. The relay connects the TCP ports between the two computers and does not store any information (and all information passing through it is encrypted). 

**New version released June 24th, 2018 - please upgrade if you are using the public relay.**

I hear you asking, *Why another open-source peer-to-peer file transfer utilities?* [There](https://github.com/cowbell/sharedrop) [are](https://github.com/webtorrent/instant.io) [great](https://github.com/kern/filepizza) [tools](https://github.com/warner/magic-wormhole) [that](https://github.com/zerotier/toss) [already](https://github.com/ipfs/go-ipfs) [do](https://github.com/zerotier/toss) [this](https://github.com/nils-werner/zget). But, after review, [I found it was useful to make another](https://schollz.github.io/sending-a-file/). Namely, *croc* has no dependencies (just [download a binary and run](https://github.com/schollz/croc/releases/latest)), it works on any operating system, and its blazingly fast because it does parallel transfer over multiple TCP ports.

# Example

_These two gifs should run in sync if you force-reload (Ctl+F5)_

**Sender:**

![send](https://raw.githubusercontent.com/schollz/croc/master/logo/sender.gif)

**Receiver:**

![receive](https://raw.githubusercontent.com/schollz/croc/master/logo/receiver.gif)


**Sender:**

```
$ croc -send some-file-or-folder
Sending 4.4 MB file named 'some-file-or-folder'
Code is: cement-galaxy-alpha

Your public key: ecad-bakery-cup-unlit-roam-fetid-arulo-updike
Recipient public key: bike-cokery-casina-donut-field-farrow-mega-shine
ok? (y/n): y

Sending (->[1]63982)..
  89% |███████████████████████████████████     | [12s:1s]
File sent (2.6 MB/s)
```

**Receiver:**

```
$ croc
Enter receive code: cement-galaxy-alpha
Receiving file (4.4 MB) into: some-file-or-folder

Your public key: bike-cokery-casina-donut-field-farrow-mega-shine
Recipient public key: ecad-bakery-cup-unlit-roam-fetid-arulo-updike
ok? (y/n): y

Receiving (<-[1]63975)..
  97% |██████████████████████████████████████  | [13s:0s]
Received file written to some-file-or-folder (2.6 MB/s)
```

Note, by default, you don't need any arguments for receiving! This makes it possible for you to just double click the executable to run (nice for those of us that aren't computer wizards).

## Using *croc* in pipes

You can easily use *croc* in pipes when you need to send data through stdin or get data from stdout.

**Sender:**

```
$ cat some_file_or_folder | croc
```

In this case *croc* will automatically use the stdin data and send and assign a filename like "croc-stdin-123456789".

**Receiver:**

```
$ croc --code code-phrase --yes --stdout | more
```

Here the reciever specified the code (`--code`) so it will not be prompted, and also specified `--yes` so the file will be automatically accepted. The output goes to stdout when flagged with `--stdout`.


# Install

[Download the latest release for your system](https://github.com/schollz/croc/releases/latest).

Or, you can [install Go](https://golang.org/dl/) and build from source with `go get github.com/schollz/croc`.


# How does it work?

*croc* is similar to [magic-wormhole](https://github.com/warner/magic-wormhole#design) in spirit. Like *magic-wormhole*, *croc* generates a code phrase for you to share with your friend which allows secure end-to-end transfering of files and folders through a intermediary relay that connects the TCP ports between the two computers. The standard relay is on a public IP address (default `cowyo.com`), but before transmitting the file the two instances of *croc* send out UDP broadcasts to determine if they are both on the local network, and use a local relay instead of the cloud relay in the case that they are both local.

The code phrase allows the relay to match the receiver and the sender. Once matched, the relay shares the public keys so that the sender and recipient can further authenticate whether or not they have the right person. Once both sides verify and consent to the transfer, then the sender will encrypt the data using the recipient's public key, so that only they can decrypt the data. After a succesful transfer, the public key is stored and next time it is not prompted and automatically trusted (TOFU).

The transfer uses [Go channels](https://golang.org/doc/effective_go.html?h=chan#channels) and parallel connections to pipe all the data. After the transmission the channels are destroyed and all the connection and meta data information is wiped from the relay server. The encrypted file data never is stored on the relay.

**Security**

The first time you use croc you will generate a unique NaCl box keypair (which uses Curve25519, XSalsa20 and Poly1305) that is unique to your computer. This keypair is used to transfer the encryption key to the recipient, and gaurantees that only the recipient can decrypt the encryption key to decrypt the file data. The encryption key is a cryptographically generated random 20 characters. The file data is encrypted using the encryption key with AES-256. This method gaurantees that all the file data going over the wire is secure, and that the only person who can decrypt it is the recipient (i.e. a MITM attacker cannot decrypt it without the keypair on the recipient's computer).

The keypair also serves as a second method of authentication. After both the sender and recipient enter their code phrases, they will be able to see each other's public keys. If the public key of the other person does not match what they say it should be (i.e. an attacker is trying to use the same code phrase to get your file - possible though unlikely), then you can cancel the transfer.

On the receiver's computer, each piece of received encrypted data is written to a separate file. These files are concatenated and then decrypted. The hash of the decrypted file is then checked against the hash transmitted from the sender (part of the meta data block).

## Run your own relay

*croc* relies on a TCP relay to staple the parallel incoming and outgoing connections. The relay temporarily stores connection information and the encrypted meta information. The default uses a public relay at, `cowyo.com`, which has a 30-day uptime of 99.989% ([click here to check the current status of the public relay](https://stats.uptimerobot.com/lOwJYIgRm)).

You can also run your own relay, it is very easy. On your server, `your-server.com`, just run

```
$ croc -relay
```

Now, when you use *croc* to send and receive you should add `-server your-server.com` to use your relay server. Make sure to open up TCP ports 27001-27009.

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
