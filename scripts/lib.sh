# ensure_go_binary verifies that a binary exists in $PATH corresponding to the
# given go-gettable URI. If no such binary exists, it is fetched via `go get`.
# Reference: https://github.com/golang/pkgsite/blob/65d33554b34b666d37b22bed7de136b562d5dba8/all.bash#L93-L103
# Copyright 2019 The Go Authors.
ensure_go_binary() {
  local binary=$(basename $1)
  if ! [ -x "$(command -v $binary)" ]; then
    echo "Installing: $1"
    # Run in a subshell for convenience, so that we don't have to worry about
    # our PWD.
    (set -x; cd && go install $1@latest)
  fi
}
