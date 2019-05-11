
<p align="center">
<img
    src="https://user-images.githubusercontent.com/6550035/46709024-9b23ad00-cbf6-11e8-9fb2-ca8b20b7dbec.jpg"
    width="408px" border="0" alt="croc">
<br>
<a href="https://github.com/schollz/croc/releases/latest"><img src="https://img.shields.io/badge/version-v6.0.3-brightgreen.svg?style=flat-square" alt="Version"></a>
<a href="https://travis-ci.org/schollz/croc"><img
src="https://img.shields.io/travis/schollz/croc.svg?style=flat-square" alt="Build
Status"></a> 
<a href="https://saythanks.io/to/schollz"><img src="https://img.shields.io/badge/Say%20Thanks-!-brightgreen.svg?style=flat-square" alt="Say thanks"></a>
</p>


<p align="center"><code>curl https://getcroc.schollz.com | bash</code></p>

`croc` is a tool that allows any two computers to simply and securely transfer files and folders. AFAIK, *croc* is the only CLI file-transfer tool does **all** of the following:

- allows **any two computers** to transfer data (using a relay)
- provides **end-to-end encryption** (using PAKE)
- enables easy **cross-platform** transfers (Windows, Linux, Mac)
- allows **multiple file** transfers
- allows **resuming transfers** that are interrupted
- does *not* require a server or port-forwarding

For more information about `croc`, see [my blog post](https://schollz.com/software/croc6).


## Install

Download [the latest release for your system](https://github.com/schollz/croc/releases/latest), or install a release from the command-line:

```
$ curl https://getcroc.schollz.com | bash
```


On macOS you can install the latest release with [Homebrew](https://brew.sh/): 

```
$ brew install schollz/tap/croc
```


On Windows you can install the latest release with [Scoop](https://scoop.sh/): 

```
$ scoop bucket add schollz-bucket https://github.com/schollz/scoop-bucket.git
$ scoop install croc
```


Or, you can [install Go](https://golang.org/dl/) and build from source (requires Go 1.11+): 

```
$ go get -v github.com/schollz/croc/v6
```



## Usage 

To send a file, simply do: 

```
$ croc send [file(s)-or-folder]
Sending 'file-or-folder' (X MB)
Code is: code-phrase
```

Them to receive the file (or folder) on another computer, you can just do 

```
$ croc code-phrase
```

The code phrase is used to establish password-authenticated key agreement ([PAKE](https://en.wikipedia.org/wiki/Password-authenticated_key_agreement)) which generates a secret key for the sender and recipient to use for end-to-end encryption.


### Custom code phrase

You can send with your own code phrase (must be more than 4 characters).

```
$ croc send --code [code-phrase] [file(s)-or-folder]
```


### Use pipes - stdin and stdout

You can pipe to `croc`:

```
$ cat [filename] | croc send
```

In this case `croc` will automatically use the stdin data and send and assign a filename like "croc-stdin-123456789". To receive to `stdout` at you can always just use the `--yes`  will automatically approve the transfer and pipe it out to `stdout`. 

```
$ croc --yes [code-phrase] > out
```

All of the other text printed to the console is going to `stderr` so it will not interfere with the message going to `stdout`.

### Self-host relay

The relay is needed to staple the parallel incoming and outgoing connections. By default, `croc` uses a public relay but you can also run your own relay:

```
$ croc relay
```

Make sure to open up TCP ports (see `croc relay --help` for which ports to open). 

You can send files using your relay by entering `--relay` to change the relay that you are using if you want to custom host your own.

```
$ croc --relay "myrelay.example.com:9009" send [filename]
```


## License

MIT

## Acknowledgements

`croc` has been through many iterations, and I am awed by all the great contributions! If you feel like contributing, in any way, by all means you can send an Issue, a PR, ask a question, or tweet me ([@yakczar](http://ctt.ec/Rq054)).

Thanks [@warner](https://github.com/warner) for the [idea](https://github.com/warner/magic-wormhole), [@tscholl2](https://github.com/tscholl2) for the [encryption gists](https://gist.github.com/tscholl2/dc7dc15dc132ea70a98e8542fefffa28), [@skorokithakis](https://github.com/skorokithakis) for [code on proxying two connections](https://www.stavros.io/posts/proxying-two-connections-go/). Finally thanks for making pull requests [@meyermarcel](https://github.com/meyermarcel), [@Girbons](https://github.com/Girbons), [@techtide](https://github.com/techtide), [@heymatthew](https://github.com/heymatthew), [@Lunsford94](https://github.com/Lunsford94), [@lummie](https://github.com/lummie), [@jesuiscamille](https://github.com/jesuiscamille), [@threefjord](https://github.com/threefjord), [@marcossegovia](https://github.com/marcossegovia), [@csleong98](https://github.com/csleong98), [@afotescu](https://github.com/afotescu), [@callmefever](https://github.com/callmefever), [@El-JojA](https://github.com/El-JojA), [@anatolyyyyyy](https://github.com/anatolyyyyyy), [@goggle](https://github.com/goggle), [@smileboywtu](https://github.com/smileboywtu), [@nicolashardy](https://github.com/nicolashardy)!
