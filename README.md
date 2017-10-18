# croc

*File transfer over parallel TCP with a rendezvous server.*

This is more or less a Golang port of [magic-wormhole](https://github.com/warner/magic-wormhole) except it probably isn't secure.

# Install

```
go get github.com/schollz/croc
```

# Basic usage

## Send a file

On computer 1 do:

```
$ croc -send somefile
Your code phrase is now limbo-rocket-gibson
waiting for other to connect
```

## Receive a file

Just type `croc` and you'll be prompted for the code phrase. Use the code phrase to get the file.

```
$ croc 
What is your code phrase? limbo-rocket-gibson
   0s [====================================================] 100%

Downloaded somefile!
```

# Advanced usage

## Make your own rendezvous server

On some server you have, `your-server.com`, just run

```
$ croc -relay
```

Now, when you use *croc* to send and receive you can add `-server your-server.com` to use your rendezvous server.


# Known issues / To-Do list

- [ ] Is it secure? not sure
- [ ] Need to clear clients who disconnect before sending/receiving
- [ ] Handle file sizes < BUFFERSIZE * numberOfConnections (currently 4096 bytes)