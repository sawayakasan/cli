package v6

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"regexp"
	"time"

	"code.cloudfoundry.org/cli/command"
	"code.cloudfoundry.org/cli/command/flag"
	"code.cloudfoundry.org/cli/command/translatableerror"
	"code.cloudfoundry.org/cli/command/v6/shared"
)

//go:generate counterfeiter . CurlActor

type CurlActor interface {
	Do(req *http.Request) (*http.Response, error)
}

type CurlCommand struct {
	RequiredArgs          flag.APIPath    `positional-args:"yes"`
	CustomHeaders         []string        `short:"H" description:"Custom headers to include in the request, flag can be specified multiple times"`
	HTTPMethod            string          `short:"X" description:"HTTP method (GET,POST,PUT,DELETE,etc)"`
	HTTPData              flag.PathWithAt `short:"d" description:"HTTP data to include in the request body, or '@' followed by a file name to read the data from"`
	IncludeReponseHeaders bool            `short:"i" description:"Include response headers in the output"`
	OutputFile            flag.Path       `long:"output" description:"Write curl body to FILE instead of stdout"`
	usage                 interface{}     `usage:"CF_NAME curl PATH [-iv] [-X METHOD] [-H HEADER] [-d DATA] [--output FILE]\n\n   By default 'CF_NAME curl' will perform a GET to the specified PATH. If data\n   is provided via -d, a POST will be performed instead, and the Content-Type\n   will be set to application/json. You may override headers with -H and the\n   request method with -X.\n\n   For API documentation, please visit http://apidocs.cloudfoundry.org.\n\nEXAMPLES:\n   CF_NAME curl \"/v2/apps\" -X GET -H \"Content-Type: application/x-www-form-urlencoded\" -d 'q=name:myapp'\n   CF_NAME curl \"/v2/apps\" -d @/path/to/file"`
	Config                command.Config
	UI                    command.UI
	Actor                 CurlActor
}

func (cmd *CurlCommand) Setup(config command.Config, ui command.UI) error {
	cmd.Config = config
	cmd.UI = ui

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cmd.Config.SkipSSLValidation(),
		},
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			KeepAlive: 30 * time.Second,
			Timeout:   cmd.Config.DialTimeout(),
		}).DialContext,
	}

	client := http.Client{
		Transport: tr,
	}
	cmd.Actor = &client
	return nil
}

func (cmd CurlCommand) Execute(args []string) error {
	if !cmd.Config.CurlExperimental() {
		return translatableerror.UnrefactoredCommandError{}
	}

	if len(args) > 0 {
		return translatableerror.TooManyArgumentsError{
			ExtraArgument: args[0],
		}
	}

	cleanedPath := filepath.Clean(cmd.RequiredArgs.Path)
	path, err := url.Parse(cleanedPath)
	host, err := url.Parse(cmd.Config.Target())
	combinedURL := host.ResolveReference(path)

	req, err := http.NewRequest(http.MethodGet, combinedURL.String(), nil)

	req.Header.Set("User-Agent", shared.BuildUserAgent(cmd.Config))

	accessToken := cmd.Config.AccessToken()
	if accessToken != "" {
		req.Header.Set("Authorization", accessToken)
	}

	verbose, _ := cmd.Config.Verbose()
	if verbose {
		cmd.UI.DisplayText(fmt.Sprintf("REQUEST: [%s]", time.Now().Format(time.RFC3339)))
		reqBytes, dumpErr := httputil.DumpRequestOut(req, false)
		redactMatcher := regexp.MustCompile("Authorization: .*")
		redactedReq := redactMatcher.ReplaceAll(reqBytes, []byte("Authorization: [PRIVATE DATA HIDDEN]"))
		cmd.UI.DisplayText(string(redactedReq))
		if dumpErr != nil {
			return dumpErr
		}
	}

	resp, err := cmd.Actor.Do(req)
	if err != nil {
		return err
	}

	if verbose {
		cmd.UI.DisplayText(fmt.Sprintf("RESPONSE: [%s]", time.Now().Format(time.RFC3339)))
		respBytes, dumpErr := httputil.DumpResponse(resp, false)
		if dumpErr != nil {
			return dumpErr
		}
		cmd.UI.DisplayText(string(respBytes))
	}
	_, err = io.Copy(cmd.UI.GetOut(), resp.Body)
	return err
}
