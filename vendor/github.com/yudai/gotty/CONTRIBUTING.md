# How to contribute

GoTTY is MIT licensed and accepts contributions via GitHub pull requests. We also accepts feature requests on GitHub issues.

## Reporting a bug

Reporting a bug is always welcome and one of the best ways to contribute. A good bug report helps the developers to improve the product much easier. We therefore would like to ask you to fill out the quesions on the issue template as much as possible. That helps us to figure out what's happening and discover the root cause.


## Requesting a new feature

When you find that GoTTY cannot fullfill your requirements because of lack of ability, you may want to open a new feature request. In that case, please file a new issue with your usecase and requirements.


## Opening a pull request

### Code Style

Please run `go fmt` on your Go code and make sure that your commits are organized for each logical change and your commit messages are in proper format (see below).

[Go's official code style guide](https://github.com/golang/go/wiki/CodeReviewComments) is also helpful.

### Format of the commit message

When you write a commit message, we recommend include following information to make review easier and keep the history cleaerer.

* What is the change
* The reason for the change

The following is an example:

```
Add something new to existing package

Since the existing function lacks that mechanism for some purpose,
this commit adds a new structure to provide it.
```

When your pull request is to add a new feature, we recommend add an actual usecase so that we can discuss the best way to achive your requirement. Opening a proposal issue in advance is another good way to start discussion of new features.


## Contact

If you have a trivial question about GoTTY for a bug or new feature, you can contact @i_yudai on Twitter (unfortunately, I cannot provide support on GoTTY though).
