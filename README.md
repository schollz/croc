# personalportal

*File transfer over parallel TCP with a rendevous server.*

This program pays homage to [magic-wormhole](https://github.com/warner/magic-wormhole) except it doesn't have the rendevous server, or the transit relay, or the password-authenticated key exchange. Its not really anything like it, except that its file transfer over TCP. Here you can transfer a file using multiple TCP ports simultaneously. 

## Normal use

### Server computer 

Be sure to open up TCP ports 27001-27009 on your port forwarding. Also, get your public address:

```
$ curl icanhazip.com
X.Y.W.Z
```

Then get and run *wormhole*:

```
$ go get github.com/schollz/wormhole
$ wormhole -file SOMEFILE
```

*personalportal* automatically knows to run as a server when the `-file` flag is set.

### Client computer

```
$ go get github.com/schollz/wormhole
$ wormhole -server X.Y.W.Z
```

*personalportal* automatically knows to run as a client when the `-server` flag is set.


## Building for use without flags

For people that don't have or don't want to build from source and don't want to use the command line, you can build it for them to have the flags set automatically! Build the wormhole binary so that it always behaves as a client to a specified server, so that someone just needs to click on it.

```
cd $GOPATH/src/github.com/schollz/wormhole
go build -ldflags "-s -w -X main.serverAddress=X.Y.W.Z" -o client.exe
```

Likewise you could do the same for the server:

```
cd $GOPATH/src/github.com/schollz/wormhole
go build -ldflags "-s -w -X main.fileName=testfile" -o server.exe
```

# License

MIT