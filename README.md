
<p align="center">
<img
    src="https://user-images.githubusercontent.com/6550035/31846899-2b8a7034-b5cf-11e7-9643-afe552226c59.png"
    width="100%" border="0" alt="croc">
<br>
<a href="https://github.com/schollz/croc/releases/latest"><img src="https://img.shields.io/badge/version-4.0.0-brightgreen.svg?style=flat-square" alt="Version"></a>
<a href="https://saythanks.io/to/schollz"><img src="https://img.shields.io/badge/Say%20Thanks-!-yellow.svg?style=flat-square" alt="Go Report Card"></a>
</p>


<p align="center">Easily and securely transfer stuff from one computer to another.</p>

*croc* allows any two computers to directly and securely transfer files and folders. When sending a file, *croc* generates a random code phrase which must be shared with the recipient so they can receive the file. The code phrase encrypts all data and metadata and also serves to authorize the connection between the two computers in a intermediary relay. The relay connects the TCP ports between the two computers and does not store any information (and all information passing through it is encrypted). 

I hear you asking, *Why another open-source peer-to-peer file transfer utilities?* [There](https://github.com/cowbell/sharedrop) [are](https://github.com/webtorrent/instant.io) [great](https://github.com/kern/filepizza) [tools](https://github.com/warner/magic-wormhole) [that](https://github.com/zerotier/toss) [already](https://github.com/ipfs/go-ipfs) [do](https://github.com/zerotier/toss) [this](https://github.com/nils-werner/zget). But, after review, [I found it was useful to make another](https://schollz.github.io/sending-a-file/). Namely, *croc* has no dependencies (just [download a binary and run](https://github.com/schollz/croc/releases/latest)), it works on any operating system, and its blazingly fast because it does parallel transfer over multiple TCP ports.

## Example

_These two gifs should run in sync if you force-reload (Ctl+F5)_

**Sender:**

![send](https://github.com/schollz/croc/blob/6af10ad871d929ace4664ac8c1e4acc37d01b323/src/testing_data/sender.gif)

**Receiver:**

![receive](https://github.com/schollz/croc/blob/6af10ad871d929ace4664ac8c1e4acc37d01b323/src/testing_data/recipient.gif)


## Install

[Download the latest release for your system](https://github.com/schollz/croc/releases/latest).

Or, you can [install Go](https://golang.org/dl/) and build from source with `go get github.com/schollz/croc`.


## Usage 

The basic usage is to just do 

```
$ croc send FILE
```

to send and then on the other computer you can just do 

```
$ croc [code phrase]
```

to receive (you'll be prompted to enter the code phrase). Note, by default, you don't need any arguments for receiving, instead you will be prompted to enter the code phrase. This makes it possible for you to just double click the executable to run (nice for those of us that aren't computer wizards).

### Custom code phrase

You can send with your own code phrase (must be more than 4 characters).

```
$ croc send --code [code phrase] [filename]
```

### Use locally

*croc* automatically will attempt to start a local connection on your LAN to transfer the file much faster. It uses [peer discovery](https://github.com/schollz/peerdiscovery), basically broadcasting a message on the local subnet to see if another *croc* user wants to receive the file. *croc* will utilize the first incoming connection from either the local network or the public relay and follow through with PAKE.

You can change this behavior by forcing *croc* to use only local connections (`--local`) or force to use the public relay only (`--no-local`):

```
$ croc --local/--no-local send [filename]
```

### Using pipes - stdin and stdout

You can easily use *croc* in pipes when you need to send data through stdin or get data from stdout. To send you can just use pipes:

```
$ cat [filename] | croc send
```

In this case *croc* will automatically use the stdin data and send and assign a filename like "croc-stdin-123456789". To receive to stdout at you can always just use the `-yes` and `-stdout` flags which will automatically approve the transfer and pipe it out to stdout. 

```
$ croc -yes -stdout [code phrase] > out
```

All of the other text printed to the console is going to `stderr` so it will not interfere with the message going to stdout.

### Self-host relay

The relay is needed to staple the parallel incoming and outgoing connections. The relay temporarily stores connection information and the encrypted meta information. The default uses a public relay at, `ws://198.199.67.130:8153`. You can also run your own relay, it is very easy, just run:

```
$ croc relay
```

Make sure to open up TCP ports (see `croc relay --help` for which ports to open). Relays can also be customized to which elliptic curve they will use (default is siec).

You can send files using your relay by entering `-relay` to change the relay that you are using if you want to custom host your own.

```
$ croc -relay "ws://myrelay.example.com" send [filename]
```


## How does it work? 

*croc* is similar to [magic-wormhole](https://github.com/warner/magic-wormhole#design) in spirit and design. Like *magic-wormhole*, *croc* generates a code phrase for you to share with your friend which allows secure end-to-end transferring of files and folders through a intermediary relay that connects the TCP ports between the two computers. Also like *magic-wormhole*, security is enabled by performing password-authenticated key exchange (PAKE) with the weak code phrase to generate a session key on both machines without passing any private information between the two. The session key is then verified and used to encrypt the content and meta-data with AES-256. If at any point the PAKE fails, an error will be reported and the file will not be transferred. More details on the PAKE transfer can be found at [github.com/schollz/pake](https://github.com/schollz/pake) and below in the [protocol](#protocol).

The intermediary relay uses websockets to have bidirectional communication with potential senders and recipients. Only one sender and one recipient is allowed in a channel at once. The relay helps to deliver the PAKE information, and once both parties agree, it will staple the connections between the sender and recipient and pipe all incoming TCP data from the sender to the recipient. 

When sending a file with *croc*, a local relay is initiated which will try to discover local peers for connection. If a recipient is found locally, then the local connections are used and the public relay is never used. This way, *croc* can be used without a public internet connection (i.e. to transfer files over LAN).

## License

MIT

## Acknowledgements

I am awed by all the [great contributions](#acknowledgements) made! If you feel like contributing, in any way, by all means you can send an Issue, a PR, ask a question, or tweet me ([@yakczar](http://ctt.ec/Rq054)).

Thanks...

- ...[@warner](https://github.com/warner) for the [idea](https://github.com/warner/magic-wormhole).
- ...[@tscholl2](https://github.com/tscholl2) for the [encryption gists](https://gist.github.com/tscholl2/dc7dc15dc132ea70a98e8542fefffa28).
- ...[@skorokithakis](https://github.com/skorokithakis) for [code on proxying two connections](https://www.stavros.io/posts/proxying-two-connections-go/).
- ...for making pull requests [@Girbons](https://github.com/Girbons), [@techtide](https://github.com/techtide), [@heymatthew](https://github.com/heymatthew), [@Lunsford94](https://github.com/Lunsford94), [@lummie](https://github.com/lummie), [@jesuiscamille](https://github.com/jesuiscamille), [@threefjord](https://github.com/threefjord), [@marcossegovia](https://github.com/marcossegovia), [@csleong98](https://github.com/csleong98), [@afotescu](https://github.com/afotescu), [@callmefever](https://github.com/callmefever), [@El-JojA](https://github.com/El-JojA), [@anatolyyyyyy](https://github.com/anatolyyyyyy), [@goggle](https://github.com/goggle), [@smileboywtu](https://github.com/smileboywtu)!
