

linux:
	go get  github.com/schollz/croc/v6@win
	go build -v -o croc

linuxprereqs:
	sudo apt-get install libgl1-mesa-dev xorg-dev
	sudo apt-get install libglib2.0-dev
	sudo apt-get install libgtk-3-dev
	
windows:
	go get  github.com/schollz/croc/v6@win
	go build -v -ldflags -H=windowsgui -o croc.exe
