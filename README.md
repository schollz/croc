```
On one computer do:

git clone https://github.com/schollz/croc.git && cd croc && git checkout v7
go build -v
cp croc croc1 # this is the file for sending, ~ 20 MB
./croc --send


On another computer do:

git clone https://github.com/schollz/croc.git && cd croc && git checkout v7
go build -v
./croc --receive

If the transfer goes through then the sender will say "transfering file"
```

