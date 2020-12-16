# ensure_go_binary verifies that a binary exists in $PATH corresponding to the
# given go-gettable URI. If no such binary exists, it is fetched via `go get`.
# Reference: https://github.com/golang/pkgsite/blob/0cd9aaec035d6ec4939ecb8efcc98379ec1f98db/all.bash#L51-L61
# Copyright 2019 The Go Authors.
ensure_go_binary() {
  local binary=$(basename $1)
  if ! [ -x "$(command -v $binary)" ]; then
    echo "Installing: $1"
    # Run in a subshell for convenience, so that we don't have to worry about
    # our PWD.
    (set -x; cd && env GO111MODULE=on go get -u $1)
  fi
}
