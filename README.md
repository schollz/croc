```
# in the first terminal
git clone https://github.com/schollz/croc.git
cd croc 
git checkout v7
cd src/webrtc/
make sender

# in second terminal
cd src/webrtc/
make receive

# open up localhost:8003 and open console.
# copy the last JSON output and save it into src/webrtc/answer.json

# communication should ensue....
```

