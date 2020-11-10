module github.com/rogerwelin/cassowary

go 1.14

require (
	github.com/aws/aws-sdk-go v1.30.24
	github.com/fatih/color v1.7.0
	github.com/hashicorp/go-hclog v0.14.1
	github.com/hashicorp/go-plugin v1.4.0
	github.com/mattn/go-colorable v0.1.4 // indirect
	github.com/mattn/go-isatty v0.0.10 // indirect
	github.com/prometheus/client_golang v1.3.0
	github.com/schollz/progressbar v1.0.0
	github.com/urfave/cli/v2 v2.2.0
)

replace github.com/rogerwelin/cassowary => ./
