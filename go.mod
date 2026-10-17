module github.com/screwyprof/prettycov

go 1.26.0

// 1.27.0 and 1.27.1 miscount statements (golang/go#80974).
toolchain go1.27.2

require (
	github.com/alecthomas/kong v1.16.1
	github.com/stretchr/testify v1.12.1
	golang.org/x/sys v0.49.0
	golang.org/x/term v0.47.0
	golang.org/x/tools v0.52.0
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect
