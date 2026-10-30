package receivefs

import (
	"errors"
	"path"
)

// CanPublishNew checks the same root-to-destination link used by PublishNew.
// Unsupported filesystems and separate mounts can use individual transfers;
// permission, I/O and cleanup failures remain errors before requesting data.
func (r *Root) CanPublishNew(folder string) (bool, error) {
	return r.probePublishNew(folder, r.root.Link)
}

func (r *Root) probePublishNew(folder string, link func(string, string) error) (supported bool, result error) {
	if err := r.MkdirAll(folder, 0o777); err != nil {
		return false, err
	}
	source, err := r.CreateTempDir(".", ".croc-bundle-probe-")
	if err != nil {
		return false, err
	}
	defer func() { result = errors.Join(result, r.RemoveAll(source)) }()
	destination, err := r.CreateTempDir(folder, ".croc-bundle-probe-")
	if err != nil {
		return false, err
	}
	defer func() { result = errors.Join(result, r.RemoveAll(destination)) }()
	file, name, err := r.CreateTemp(source, "probe-", 0o600)
	if err != nil {
		return false, err
	}
	if err = file.Close(); err != nil {
		return false, err
	}
	from, err := native(name, false)
	if err != nil {
		return false, err
	}
	to, err := native(path.Join(destination, "probe"), false)
	if err != nil {
		return false, err
	}
	if err = link(from, to); err != nil {
		if publicationUnsupported(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
