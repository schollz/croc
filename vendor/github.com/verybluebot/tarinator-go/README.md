# Tarinator-go
## Genaral
Tarinator-go a Golang package that simplifies creating tar files and compressing/decompressing
them using gzip.

Here is an example for using Tarinator-go (including a tutorial for building it):

https://github.com/verybluebot/cli_tarinator_example

## Usage
At this point it can create .tar and tar.gz files from unlimited number of files and
directories.


### Creat Tar file:
creating `.tar` file from a list of files and/or directories:

```
// create an []string of paths to your files and directories

import(
    "github.com/verybluebot/tarinator-go"
)

paths := []string{
    "someFile.txt",
    "someOtherFile.json",
    "someDir/",
    "some/path/to/dir/",
}

err := tarinator.Tarinate(paths, "your_tar_file.tar")
if err != nil {
    // handle error
}
```

For creating `.tar.gz` file use `.tar.gz` to the file name aka `your_tar_file.tar.gz`.

### Extarcing a tar file
For extarcting the tar file just give input the file path and the destenetion to extract
in the example below the tar file is in `/home/someuser/some_tar.tar` and the destenation is `/tmp/things/`.
```
import(
    "github.com/verybluebot/tarinator-go"
)

err := tarinator.UnTarinate("/home/someuser/some_tar.tar", "/tmp/things/")
if err != nil {
    // handle error
}
```

For extracting `.tar.gz` files just specify a `.tar.gz` file name and Tarinator-go will recognize it.

## Thanks to
Svett Ralchev for [this blog post](http://blog.ralch.com/tutorial/golang-working-with-tar-and-gzip/) which helped in creation of Tarinator-go


## Licence
[MIT](https://github.com/verybluebot/cli_tarinator_example/blob/master/LICENCE.md)
