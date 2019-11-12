```
    Clients connect to websockets and broadcasts "ready".
    If client receives "ready" then it becomes the rtcOfferer and initiates (note that being an offerer does not mean it is the sender, that will be established later).
    Establish secure passphrase using PAKE
    Establish RTC communication
    Communication moves to the RTC channel


1: rtcOfferer


    createOffer
    setLocalDescription
    SEND offer
    RECIEVE answer
    setRemoteDescription(answer)


2: rtcAnswerer


    setLocalDescription
    RECEIVE offer
    setRemoteDescription(offer)
    createAnswer
    SEND answer
```

