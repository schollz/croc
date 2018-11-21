REM  getting the output of an executed command in a batch file is not trivial, we use a temp file for it
git describe --tags --abbrev=0 > temp.txt
set /P VERSION=< temp.txt
echo %VERSION%
del temp.txt

REM  build a 32 bit Windows application, this way it will run on both 32 but and 64 bit Windows machines
set GOARCH=386

REM  -s and -w strip the program of debugging information, making it smaller
REM  -H=windowsgui makes the program not have a console window on start-up
go build -ldflags="-s -w -H=windowsgui -X main.Version=%VERSION%" -o croc.exe

if errorlevel 1 pause
