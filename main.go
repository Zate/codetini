package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	. "github.com/logrusorgru/aurora"
	log "github.com/sirupsen/logrus"
)

var (
	// Home is the home directory of the current user
	Home = os.Getenv("HOME")
	// URL to download latest code-server binary for linux
	URL = "https://codesrv-ci.cdr.sh/"
	// Bin is the name of the linux binary to download
	Bin = "latest-linux"
	// UserDir is where vscode stores it's User preferences
	UserDir = Home + "/.config/Code/User"
	// CodeExtDir is where code-server stores it's extensions
	CodeExtDir = Home + "/.local/share/code-server/extensions"
	// CodeUserDir is where code-server stores its user preferences
	CodeUserDir = Home + "/.local/share/code-server/User"
	// VSCodeExtDir is where VSCode stores its extensions
	VSCodeExtDir = Home + "/.vscode/extensions"
	// VSCodeOSSExtDir is where the OSS version of vscode stores its extenstions
	VSCodeOSSExtDir = Home + "/.vscode-oss/extensions"
	// CodeBinDir is where we will store the code-server-linux binary
	CodeBinDir = Home + "/.local/share/code-server/bin"
	// CodeBin is the name of the binary to run locally
	CodeBin = "code-server-linux"
	// CBIN is thefull path to the code-server binary
	CBIN = CodeBinDir + "/" + CodeBin
	// Port is the port we will run the code-server on
	Port = 1337
	// DefaultPerms for files/directories we create
	DefaultPerms = 0770
)

// <ListBucketResult xmlns="http://doc.s3.amazonaws.com/2006-03-01">
// 	<Name>codesrv-ci.cdr.sh</Name>
// 	<Prefix/>
// 	<Marker/>
// 	<IsTruncated>false</IsTruncated>
// 	<Contents>
// 		<Key>latest-linux</Key>
// 		<Generation>1555723940832425</Generation>
// 		<MetaGeneration>1</MetaGeneration>
// 		<LastModified>2019-04-20T01:32:20.832Z</LastModified>
// 		<ETag>"d85a301acee0a0749660a802767c95c3"</ETag>
// 		<Size>94109479</Size>
// 	</Contents>

// ListBucketResult represents the XML output from an S3 list of buckets
type ListBucketResult struct {
	Name, Prefix, Marker, Delimiter string
	MaxKeys                         int64
	IsTruncated                     bool
	Contents                        []ListBucketResultContents
}

// ListBucketResultContents is for each of the contents sections in a list of buckets
type ListBucketResultContents struct {
	Key, ETag, StorageClass string `json:",omitempty"`
	Size                    int
	LastModified            time.Time `json:",omitempty"`
}

func errCheck(msg string, err error) {
	if err != nil {
		log.Printf("%s: %+v", msg, err)
		panic(err)
	}
}

func init() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

}

func checkBin(cbin string) (os.FileInfo, bool, error) {
	finfo, err := os.Stat(cbin)
	if err == nil {
		mt := finfo.ModTime().UTC()
		// get https://codesrv-ci.cdr.sh - look at the XML
		r, err := http.Get(URL)
		errCheck("xml download: ", err)
		defer r.Body.Close()
		// we can check if the LastModified time is after the timestamp on the file inside finfo
		bxml, err := ioutil.ReadAll(r.Body)
		buckets := new(ListBucketResult)
		xml.Unmarshal(bxml, &buckets)
		for i := 0; i < len(buckets.Contents); i++ {
			if buckets.Contents[i].Key == Bin {
				ut := buckets.Contents[i].LastModified.UTC()
				if mt.UTC().After(ut) {
					log.Println(Bold(BrightGreen("Binary is healthy and up to date.").BgBlack()), Bold(" Release the hounds."))
					return finfo, true, nil
				}
			}
			log.Fatalln(Bold(BrightRed("No")), Bold(BrightRed(Bin)), Bold(BrightRed("found in response from")), Bold(BrightRed(URL)), Bold("Exiting Now."))
		}
	}
	return finfo, false, err
}

func checkCode(cbin string) {
	// Check if the binary exists.
	// If it does, check mod times vs the latest binary on the ci server.
	// If there is any err checking the binary, just download it and put it in place anyhow
	finfo, bin, err := checkBin(cbin)
	errCheck("Binary Check: ", err)
	if bin {
		return
	}
	log.Println(Bold(Red("Local")), Bold(BrightWhite(finfo.Name())), Bold(Red("binary is unhealthy or out of date.")), Bold("Updating Now."))
	ubin := URL + Bin
	err = os.Remove(cbin)
	errCheck("file remove: ", err)
	rd, err := http.Get(ubin)
	errCheck("http get:", err)
	defer rd.Body.Close()
	mode := os.FileMode(uint32(int(DefaultPerms)))
	err = os.MkdirAll(CodeBinDir, mode)
	fout, err := os.Create(cbin)
	errCheck("create binary: ", err)
	defer fout.Close()
	_, err = io.Copy(fout, rd.Body)
	errCheck("write file: ", err)
	err = os.Chmod(cbin, mode)
	errCheck("chmod: ", err)

}

func launchCode(cbin string) {
	// Check the port
	p, err := net.Listen("tcp", fmt.Sprintf(":%d", Port))
	if err != nil {
		log.Fatalln(Bold(BrightRed("Port:")), Bold(BrightWhite(Port)), Bold(BrightRed("is taken.")), Bold(BrightRed(URL)), Bold("Exiting Now."))

	}
	_ = p.Close()
	log.Println(Bold(BrightGreen("Port is clear.").BgBlack()), Bold("Ready to launch."))
	// Check extensions, link them if we have to
	err = doExtensions()
	errCheck("Check Extensions: ", err)
	// background launch code-server-linux with --host penguin.linux.test --allow-http --no-auth --port=Port
	// wait for code-server-linux to be up and then launch app/browser
	// Remain open /  working in the background
	cmd := exec.Command(cbin, "--host", "0.0.0.0", "--allow-http", "--no-auth", "--port="+strconv.Itoa(Port))
	err = cmd.Start()
	errCheck("Exec Command: ", err)
	pid := cmd.Process.Pid
	// use goroutine waiting, manage process
	// this is important, otherwise the process becomes in S mode
	log.Printf("Pid: %v", pid)

	www := exec.Command("www-browser", "--url", "http://penguin.linux.test:"+strconv.Itoa(Port))
	err = www.Start()
	errCheck("Exec Command: ", err)
	wpid := www.Process.Pid
	// use goroutine waiting, manage process
	// this is important, otherwise the process becomes in S mode
	log.Printf("Pid: %v", wpid)

	go func() {
		err = cmd.Wait()
		err2 := www.Wait()
		log.Printf("Command finished with error: %v", err)
		log.Printf("Command finished with error: %v", err2)
	}()
	return

}

func doExtensions() error {
	return nil
}

func main() {
	// check if code-server is the latest and update if it's not
	checkCode(CBIN)

	// launch as app / browser - default to app
	launchCode(CBIN)

}
