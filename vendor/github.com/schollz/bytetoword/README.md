# bytetoword

This will convert Go bytes to words. Every word is for one byte.


The word list is generated using

```
curl  https://raw.githubusercontent.com/schollz/BandGenerator/master/dictionary.txt > dictionary.txt
curl https://raw.githubusercontent.com/dwyl/english-words/master/words.txt >> dictionary.txt
python3 run.py
```

and then copy-paste the `words.txt` into `words.go`.

## License

Unlicense