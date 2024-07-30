package croc

import "errors"

var ErrInvalidRecipientPassword = errors.New("invalid recipient password")
var ErrCouldNotSecureChannel = errors.New("could not secure channel")
