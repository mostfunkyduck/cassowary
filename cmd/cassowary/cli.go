package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/fatih/color"
	"github.com/hashicorp/go-plugin"
	"github.com/rogerwelin/cassowary/pkg/client"
	"github.com/urfave/cli/v2"

	hclog "github.com/hashicorp/go-hclog"
)

var (
	version             = "dev"
	errConcurrencyLevel = errors.New("Error: Concurrency level cannot be set to: 0")
	errRequestNo        = errors.New("Error: No. of request cannot be set to: 0")
	errNotValidURL      = errors.New("Error: Not a valid URL. Must have the following format: http{s}://{host}")
	errNotValidHeader   = errors.New("Error: Not a valid header value. Did you forget : ?")
	errDurationValue    = errors.New("Error: Duration cannot be set to 0 or negative")
)

func outPutResults(metrics client.ResultMetrics) {
	printf(summaryTable,
		color.CyanString(fmt.Sprintf("%.2f", metrics.TCPStats.TCPMean)),
		color.CyanString(fmt.Sprintf("%.2f", metrics.TCPStats.TCPMedian)),
		color.CyanString(fmt.Sprintf("%.2f", metrics.TCPStats.TCP95p)),
		color.CyanString(fmt.Sprintf("%.2f", metrics.ProcessingStats.ServerProcessingMean)),
		color.CyanString(fmt.Sprintf("%.2f", metrics.ProcessingStats.ServerProcessingMedian)),
		color.CyanString(fmt.Sprintf("%.2f", metrics.ProcessingStats.ServerProcessing95p)),
		color.CyanString(fmt.Sprintf("%.2f", metrics.ContentStats.ContentTransferMean)),
		color.CyanString(fmt.Sprintf("%.2f", metrics.ContentStats.ContentTransferMedian)),
		color.CyanString(fmt.Sprintf("%.2f", metrics.ContentStats.ContentTransfer95p)),
		color.CyanString(strconv.Itoa(metrics.TotalRequests)),
		color.CyanString(strconv.Itoa(metrics.FailedRequests)),
		color.CyanString(fmt.Sprintf("%.2f", metrics.DNSMedian)),
		color.CyanString(fmt.Sprintf("%.2f", metrics.RequestsPerSecond)),
	)
}

func outPutJSON(fileName string, metrics client.ResultMetrics) error {
	if fileName == "" {
		// default filename for json metrics output.
		fileName = "out.json"
	}
	f, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	return enc.Encode(metrics)
}

func runLoadTest(c *client.Cassowary) error {
	metrics, err := c.Coordinate()
	if err != nil {
		return err
	}
	outPutResults(metrics)

	if c.ExportMetrics {
		return outPutJSON(c.ExportMetricsFile, metrics)
	}

	if c.PromExport {
		err := c.PushPrometheusMetrics(metrics)
		if err != nil {
			return err
		}
	}

	if c.Cloudwatch {
		session, err := session.NewSession()
		if err != nil {
			return err
		}

		svc := cloudwatch.New(session)
		_, err = c.PutCloudwatchMetrics(svc, metrics)
		if err != nil {
			return err
		}
	}

	return nil
}

func validateCLI(c *cli.Context) error {
	prometheusEnabled := false
	var header []string
	var httpMethod string
	var data []byte
	duration := 0
	var urlSuffixes []string
	fileMode := false

	if c.Int("concurrency") == 0 {
		return errConcurrencyLevel
	}

	if c.Int("requests") == 0 {
		return errRequestNo
	}

	if c.String("duration") != "" {
		var err error
		duration, err = strconv.Atoi(c.String("duration"))
		if err != nil {
			return err
		}
		if duration <= 0 {
			return errDurationValue
		}
	}

	if !client.IsValidURL(c.String("url")) {
		return errNotValidURL
	}

	if c.String("prompushgwurl") != "" {
		prometheusEnabled = true
	}

	if c.String("header") != "" {
		length := 0
		length, header = client.SplitHeader(c.String("header"))
		if length != 2 {
			return errNotValidHeader
		}
	}

	if c.String("file") != "" {
		var err error
		urlSuffixes, err = readLocalRemoteFile(c.String("file"))
		if err != nil {
			return nil
		}
		fileMode = true
	}

	if c.String("postfile") != "" {
		httpMethod = "POST"
		fileData, err := readFile(c.String("postfile"))
		if err != nil {
			return err
		}
		data = fileData
	} else if c.String("putfile") != "" {
		httpMethod = "PUT"
		fileData, err := readFile(c.String("putfile"))
		if err != nil {
			return err
		}
		data = fileData
	} else if c.String("patchfile") != "" {
		httpMethod = "PATCH"
		fileData, err := readFile(c.String("patchfile"))
		if err != nil {
			return err
		}
		data = fileData
	} else {
		httpMethod = "GET"
	}

	tlsConfig := new(tls.Config)
	if c.String("ca") != "" {
		pemCerts, err := ioutil.ReadFile(c.String("ca"))
		if err != nil {
			return err
		}
		ca := x509.NewCertPool()
		if !ca.AppendCertsFromPEM(pemCerts) {
			return fmt.Errorf("failed to read CA from PEM")
		}
		tlsConfig.RootCAs = ca
	}

	if c.String("cert") != "" && c.String("key") != "" {
		cert, err := tls.LoadX509KeyPair(c.String("cert"), c.String("key"))
		if err != nil {
			return err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	cass := &client.Cassowary{
		FileMode:          fileMode,
		BaseURL:           c.String("url"),
		ConcurrencyLevel:  c.Int("concurrency"),
		Requests:          c.Int("requests"),
		RequestHeader:     header,
		Duration:          duration,
		PromExport:        prometheusEnabled,
		TLSConfig:         tlsConfig,
		PromURL:           c.String("prompushgwurl"),
		Cloudwatch:        c.Bool("cloudwatch"),
		ExportMetrics:     c.Bool("json-metrics"),
		ExportMetricsFile: c.String("json-metrics-file"),
		DisableKeepAlive:  c.Bool("disable-keep-alive"),
		Timeout:           c.Int("timeout"),
		HTTPMethod:        httpMethod,
		URLPaths:          urlSuffixes,
		Data:              data,
	}

	return runLoadTest(cass)
}

func runCLI(args []string) {
	app := cli.NewApp()
	app.Name = "cassowary - 學名"
	app.HelpName = "cassowary"
	app.UsageText = "cassowary [command] [command options] [arguments...]"
	app.EnableBashCompletion = true
	app.Usage = ""
	app.Version = version
	app.Commands = []*cli.Command{
		{
			Name:  "run",
			Usage: "start load-test",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "u",
					Aliases:  []string{"url"},
					Usage:    "the url (absoluteURI) to be used",
					Required: true,
				},
				&cli.IntFlag{
					Name:    "c",
					Aliases: []string{"concurrency"},
					Usage:   "number of concurrent users",
					Value:   1,
				},
				&cli.IntFlag{
					Name:    "n",
					Aliases: []string{"requests"},
					Usage:   "number of requests to perform",
					Value:   1,
				},
				&cli.StringFlag{
					Name:    "f",
					Aliases: []string{"file"},
					Usage:   "file-slurp mode: specify `FILE` path, local or www, containing the url suffixes",
				},
				&cli.StringFlag{
					Name:    "d",
					Aliases: []string{"duration"},
					Usage:   "set the duration in seconds of the load test (example: do 100 requests in a duration of 30s)",
				},
				&cli.IntFlag{
					Name:    "t",
					Aliases: []string{"timeout"},
					Usage:   "http client timeout",
					Value:   5,
				},
				&cli.StringFlag{
					Name:    "p",
					Aliases: []string{"prompushgwurl"},
					Usage:   "specify prometheus push gateway url to send metrics (optional)",
				},
				&cli.BoolFlag{
					Name:    "C",
					Aliases: []string{"cloudwatch"},
					Usage:   "enable to send metrics to AWS Cloudwatch",
				},
				&cli.StringFlag{
					Name:    "H",
					Aliases: []string{"header"},
					Usage:   "add arbitrary header, eg. 'Host: www.example.com'",
				},
				&cli.BoolFlag{
					Name:    "F",
					Aliases: []string{"json-metrics"},
					Usage:   "outputs metrics to a json file by setting flag to true",
				},
				&cli.StringFlag{
					Name:  "postfile",
					Usage: "file containing data to POST (content type will default to application/json)",
				},
				&cli.StringFlag{
					Name:  "patchfile",
					Usage: "file containing data to PATCH (content type will default to application/json)",
				},
				&cli.StringFlag{
					Name:  "putfile",
					Usage: "file containing data to PUT (content type will default to application/json)",
				},
				&cli.StringFlag{
					Name:  "json-metrics-file",
					Usage: "outputs metrics to a custom json filepath, if json-metrics is set to true",
				},
				&cli.BoolFlag{
					Name:  "disable-keep-alive",
					Usage: "use this flag to disable http keep-alive",
				},
				&cli.StringFlag{
					Name:  "ca",
					Usage: "ca certificate to verify peer against",
				},
				&cli.StringFlag{
					Name:  "cert",
					Usage: "client authentication certificate",
				},
				&cli.StringFlag{
					Name:  "key",
					Usage: "client authentication key",
				},
			},
			Action: validateCLI,
		},
	}

	if err := initPlugins(); err != nil {
		log.Fatalf("error initializing plugins: %s\n", err)
	}
	err := app.Run(args)
	if err != nil {
		log.Fatalf("error running application: %s\n", err)
	}
}

func initPlugins() error {
	// Create an hclog.Logger
	logger := hclog.New(&hclog.LoggerOptions{
		Name:   "plugin",
		Output: os.Stdout,
		Level:  hclog.Debug,
	})

	// From example in docs:
	// handshakeConfigs are used to just do a basic handshake between
	// a plugin and host. If the handshake fails, a user friendly error is shown.
	// This prevents users from executing bad plugins or executing a plugin
	// directory. It is a UX feature, not a security feature.

	handshakeConfig := plugin.HandshakeConfig{
		ProtocolVersion:  1,
		MagicCookieKey:   "BASIC_PLUGIN",
		MagicCookieValue: "hello",
	}

	pluginMap := map[string]plugin.Plugin{
		"plugin": &client.PluginImpl{},
	}

	c := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		Cmd:             exec.Command("./plugin/plugin"),
		Logger:          logger,
	})
	defer c.Kill()

	// Connect via RPC
	rpcClient, err := c.Client()
	if err != nil {
		return fmt.Errorf("could not build rpc client: %s", err)
	}

	// Request the plugin
	pluginName := "plugin"
	raw, err := rpcClient.Dispense(pluginName)
	if err != nil {
		return fmt.Errorf("could not dispense rpc request to %s: %s", pluginName, err)
	}

	// We should have a Greeter now! This feels like a normal interface
	// implementation but is in fact over an RPC connection.
	plugin := raw.(client.Plugin)
	if errString := plugin.Init(); errString != "" {
		return fmt.Errorf("plugin returned error: %s", errString)
	}
	return nil
}
