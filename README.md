<p align="center">
<img
    src="croc/logo.png"
    width="260" height="80" border="0" alt="croc">
<br>
<a href="https://github.com/schollz/croc/releases/latest"><img src="https://img.shields.io/badge/version-WIP-red.svg?style=flat-square" alt="Version"></a>
<img src="https://img.shields.io/badge/coverage-7%25-red.svg?style=flat-square" alt="Code Coverage">
<a href="https://gitter.im/schollz/croc?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=body_badge"><img src="https://img.shields.io/badge/chat-on%20gitter-green.svg?style=flat-square" alt="Version"></a>
</p>

<p align="center">Secure peer-to-peer file transfer using a TCP relay.</p>

This is more or less a Golang port of [@warner](https://github.com/warner's) [*magic-wormhole*](https://github.com/warner/magic-wormhole). I wrote this because I wanted to send my friend Jessie a file using *magic-wormhole*. However, Jessie doesn't like the idea of putting Python on her computer because it is a giant snake. So, nominally, this is a version of *magic-wormhole* without the dependencies that you can just double-click on your computer, even if you use Windows.

**This is a work-in-progress, the master branch may break from time to time until the first release.**

# Example

**Sender:**

```
$ croc -send croc.exe
Sending 3712016 byte file named 'croc.exe'
Code is: 4-cement-galaxy-alpha

Sending (->24.65.41.43:50843)..
   0s [==========================================================] 100%
   0s [==========================================================] 100%
   0s [===========================================>--------------]  79%
   0s [========================================>-----------------]  75%
File sent.
```

**Receiver:**

```
$ croc 
Enter receive code: 4-cement-galaxy-alpha
Receiving file (3712016 bytes) into: croc.exe
ok? (y/n): y

Receiving (<-50.32.38.188:50843)..
   0s [==========================================================] 100%
   0s [==========================================================] 100%
   0s [==========================================================] 100%
   0s [==========================================================] 100%
Received file written to croc.exe
```

# Install

[Install Go](https://golang.org/dl/) and then:

```
go get github.com/schollz/croc
```

Or, if you are like my good friend Jessie and "*just can't even*" with programming, [download the latest release for your system](https://github.com/schollz/croc/releases/latest).


# Advanced usage

## Run your own relay

*croc* relies on a TCP relay to staple the parallel incoming and outgoing connections. The relay temporarily stores connection information and the encrypted meta information. The default uses my server, `cowyo.com`, which has no guarantees except that I guarantee to turn if off as soon as it gets abused. 

I recommend you run your own relay, it is very easy. On your server, `your-server.com`, just run

```
$ croc -relay
```

Now, when you use *croc* to send and receive you should add `-server your-server.com` to use your relay server. 

_Note:_ If you are behind a firewall, make sure to open up TCP ports 27001-27009.

# How does it work?

*croc* is similar to [magic-wormhole](https://github.com/warner/magic-wormhole#design) in spirit and design. Like *magic-wormhole*, *croc* generates a code phrase for you to share with your friend which allows secure end-to-end transfering of files. The similarities may diverge from here.

The code phrase is 16 random bits that are [menemonic encoded](http://web.archive.org/web/20101031205747/http://www.tothink.com/mnemonic/) plus a prepended integer to specify number of threads. This code phrase is hashed using sha256 and sent to the relay which maps that key to that connection. When the relay finds a matching key for both the receiver and the sender (i.e. they both have the same code phrase), then the sender transmits the encrypted metadata to the receiver through the relay. Then the receiver decrypts and reviews the metadata (file name, size), and chooses whether to consent to the transfer.

After the receiver consents to the transfer, the sender transmits encrypted data through the relay. The relay setups up [Go channels](https://golang.org/doc/effective_go.html?h=chan#channels) for each connection which pipes all the data incoming from that sender's connection out to the receiver's connection. After the transmission the channels are destroyed and all the connection and meta data information is wiped from the relay server.

**Encryption**

Encryption uses PBKDF2 (see [RFC2898](http://www.ietf.org/rfc/rfc2898.txt)) where the code phrase shared between the sender and receiver is used as the passphrase. For each of the two encrypted data blocks (metadata stored on relay server, and file data transmitted), a random 8-byte salt is used and a IV is generated according to [NIST Recommendation for Block ciphers, Section 8.2](http://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf).


**Decryption**

On the receiver's computer, each piece of received encrypted data is written to a separate file. These files are concatenated and then decrypted. The hash of the decrypted file is then checked against the hash transmitted from the sender (part of the meta data block). 

# License 

MIT

# Acknowledgements

Thanks...

- ...[@warner](https://github.com/warner) for the [idea](https://github.com/warner/magic-wormhole).
- ...[@tscholl2](https://github.com/tscholl2) for the [encryption gists](https://gist.github.com/tscholl2/dc7dc15dc132ea70a98e8542fefffa28).
- ...[@skorokithakis](https://github.com/skorokithakis) for [code on proxying two connections](https://www.stavros.io/posts/proxying-two-connections-go/).
- ...for making pull requests [@Girbons](https://github.com/ss), [@techtide](https://github.com/techtide), [@heymatthew](https://github.com/heymatthew)!