package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	pb "github.com/vagababov/prime-server/proto"
	"google.golang.org/grpc"
)

const (
	defaultPort      = "8080"
	portVariableName = "PORT"
)

var (
	backend = flag.String("backend", "http-prime.default.svc.cluster.local",
		"The k8s service name to query the backend information")
	host     = flag.String("host", "", "The host name to use if client runs outside of the cluster")
	insecure = flag.Bool("insecure", true, "true if we want to skip SSL certificate for gRPC calls")
	useGRPC  = flag.Bool("use_grpc", false, "If true, the service will use gRPC to talk to the backend")
)

const (
	koPathEnvVar = "KO_DATA_PATH"
	koPathDefVal = "./kodata/"
)

func main() {
	flag.Parse()

	// router
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	koPath := getEnv(koPathEnvVar, koPathDefVal)
	fmt.Printf("KO Path: %q\n", koPath)

	// static
	r.LoadHTMLFiles(path.Join(koPath, "index.html"))
	r.Static("/img", path.Join(koPath, "static/img"))
	r.Static("/css", path.Join(koPath, "static/css"))

	// routes
	r.GET("/", handlerDef)
	r.GET("/prime", handler)

	// port
	port := getEnv(portVariableName, defaultPort)
	addr := ":" + port
	fmt.Printf("Server starting: %s \n", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func handler(ctx *gin.Context) {
	param := ctx.DefaultQuery("query", "4")
	qint, err := strconv.ParseInt(param, 10 /*base*/, 64 /*bitcnt*/)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	query := &pb.Request{
		Query: qint,
	}

	if *useGRPC {
		doGRPC(ctx, query)
	} else {
		doHTTP(ctx, query)
	}
}

func doHTTP(ctx *gin.Context, query *pb.Request) {
	fmt.Println("HTTP pill is taken")
	b, _ := json.Marshal(query)
	buf := bytes.NewBuffer(b)

	req, _ := makeHTTPReq(buf)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	rsp, err := ReadResponse(resp.Body)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	logo := ""
	if rsp.Answer < 0 {
		logo = "img/knative-logo2.png"
	}

	ctx.HTML(http.StatusOK, "index.html", map[string]interface{}{
		"max":     query.Query,
		"result":  fmt.Sprintf("Highest prime: %d", rsp.Answer),
		"altLogo": logo,
		"motd":    "Good Ol' HTTP is in play 'ere!",
	})
}

func doGRPC(ctx *gin.Context, query *pb.Request) {
	fmt.Println("gRPC pill is taken")
	resp, err := queryGRPC(query)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	const logo = "img/knative-logo3.png"
	ctx.HTML(http.StatusOK, "index.html", map[string]interface{}{
		"max":     query.Query,
		"result":  fmt.Sprintf("Highest prime: %d", resp.Answer),
		"altLogo": logo,
		"motd":    "Brought to you the gRPC!",
	})
}

func handlerDef(ctx *gin.Context) {
	// Dummy inital values.
	ctx.HTML(http.StatusOK, "index.html", map[string]interface{}{
		"max":    4,
		"result": "Highest prime: 3",
	})
}

func ReadResponse(r io.Reader) (*pb.Response, error) {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	resp := &pb.Response{}
	err = json.Unmarshal(data, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func makeHTTPReq(b *bytes.Buffer) (*http.Request, error) {
	url := fmt.Sprintf("http://%s/", *backend)
	req, err := http.NewRequest(http.MethodPost, url, b)
	if err != nil {
		return nil, err
	}
	if *host != "" {
		req.Host = *host
	}
	return req, nil
}

func getEnv(s, d string) string {
	ret := os.Getenv(s)
	if ret == "" {
		ret = d
	}
	return ret
}

func queryGRPC(req *pb.Request) (*pb.Response, error) {
	var opts []grpc.DialOption
	if *host != "" {
		opts = append(opts, grpc.WithAuthority(*host))
	}
	if *insecure {
		opts = append(opts, grpc.WithInsecure())
	}
	fmt.Printf("Dialing to: %s\n", *backend)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, *backend, opts...)
	if err != nil {
		fmt.Printf("failed to dial: %v\n", err)
		return nil, err
	}
	defer conn.Close()

	client := pb.NewPrimeServiceClient(conn)

	resp, err := client.Get(context.Background(), req)
	if err != nil {
		fmt.Printf("Error calling Get: %+v\n", err)
		return nil, err
	}
	fmt.Printf("gRPC response is: %v\n", resp)
	return resp, nil
}
