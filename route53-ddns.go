package main

import (
    "encoding/json"
    "net"
    "net/http"
    "os"
    "path"
    "time"

    "github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/credentials"
    "github.com/aws/aws-sdk-go/aws/session"
    "github.com/aws/aws-sdk-go/service/route53"

    "github.com/kardianos/osext"

    "github.com/op/go-logging"
)

var logFileName = "route53.log"
var configFileName = "config.json"
var log = logging.MustGetLogger("dyndns")
var logFormat = logging.MustStringFormatter(
    `%{time:2006-01-02 15:04:05}   %{level:.5s}:	%{message}`,
)

type Configuration struct {
    AwsAccessKeyId      string
    AwsSecretAccessKey  string
    HostedZoneId        string
    Fqdn                string
}

type Response struct {
    Ip string
}

func perror(err error, logger *logging.Logger) {
    if err != nil {
        logger.Error(err.Error())
        panic(err)
    }
}

func main() {
    // Get the current directory
    dir, err := osext.ExecutableFolder()
    perror(err, log)


    // Initialze a log file
    logFile, err := os.OpenFile(
        path.Join(dir, logFileName),
        os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666,
    )


    // Log settings
    loggingBackend   := logging.NewLogBackend(logFile, "", 0)
    backendFormatter := logging.NewBackendFormatter(loggingBackend, logFormat)
    backendLeveled   := logging.AddModuleLevel(backendFormatter)

    // Level in logging.ERROR, logging.INFO, logging.DEBUG
    backendLeveled.SetLevel(logging.INFO, "")
    logging.SetBackend(backendLeveled)

    perror(err, log)


    // Read the config file
    configFile, err := os.Open(path.Join(dir, configFileName))
    perror(err, log)
    decoder := json.NewDecoder(configFile)
    var config Configuration
    err = decoder.Decode(&config)
    perror(err, log)

    // Request your WAN IP
    url := "https://api.ipify.org?format=json"
    client := http.Client{
        Timeout: time.Duration(30 * time.Second),
    }
    res, err := client.Get(url)
    perror(err, log)

    defer res.Body.Close()
    decoder = json.NewDecoder(res.Body)

    var body Response
    err = decoder.Decode(&body)
    perror(err, log)

    wanIp := body.Ip

    log.Debugf("Current WAN IP is '%s'", wanIp)


    // Obtain the current IP bound to the FQDN
    ips, err := net.LookupHost(config.Fqdn)
    currentIp := ""
    if ips != nil {
        currentIp = ips[0]
    }
    log.Debugf("Current IP bound to '%s' is '%s'", config.Fqdn, currentIp)

    // Update the FQDN's IP in case the current WAN IP is different from the IP
    // bounded to the FQDN
    if currentIp != wanIp {

        log.Infof("'%s' out of date, update '%s' to '%s'", config.Fqdn, currentIp, wanIp)

        var token string
        creds := credentials.NewStaticCredentials(
            config.AwsAccessKeyId,
            config.AwsSecretAccessKey,
            token,
        )

        svc := route53.New(session.New(), &aws.Config{
            Credentials: creds,
        })

        params := &route53.ChangeResourceRecordSetsInput{
            ChangeBatch: &route53.ChangeBatch{
                Changes: []*route53.Change{
                    {
                        Action: aws.String("UPSERT"),
                        ResourceRecordSet: &route53.ResourceRecordSet{
                            Name: aws.String(config.Fqdn),
                            Type: aws.String("A"),
                            ResourceRecords: []*route53.ResourceRecord{
                                {
                                    Value: aws.String(body.Ip),
                                },
                            },
                            TTL: aws.Int64(300),
                        },
                    },
                },
                Comment: aws.String("IP update by GO script from NAS"),
            },
            HostedZoneId: aws.String(config.HostedZoneId),
        }

        resp, err := svc.ChangeResourceRecordSets(params)
        perror(err, log)

        // Pretty-print the response data.
        log.Debugf("Route53 response: %v", resp)

    } else {
        log.Infof("'%s' is up to date", config.Fqdn)
    }
}
