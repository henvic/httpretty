# Adapted from @aminueza's go-github-action/fmt/fmt.sh
# Reference: https://github.com/aminueza/go-github-action/blob/master/fmt/fmt.sh
# Execute fmt tool, resolve and emit each unformatted file
UNFORMATTED_FILES=$(go fmt $(go list ./... | grep -v /vendor/))

if [ -n "$UNFORMATTED_FILES" ]; then
	echo '::error::The following files are not properly formatted:'
	echo "$UNFORMATTED_FILES" | while read -r LINE; do
		FILE=$(realpath --relative-base="." "$LINE")
		echo "::error::  $FILE"
	done
	exit 1
fi
