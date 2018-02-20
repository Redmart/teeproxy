teeproxy
=========

A reverse HTTP proxy that duplicates requests.

Why you may need this?
----------------------
You may have production servers running, but you need to upgrade to a new system. You want to run A/B test on both old and new systems to confirm the new system can handle the production load, and want to see whether the new system can run in shadow mode continuously without any issue.

How it works?
-------------
teeproxy is a reverse HTTP proxy. For each incoming request, it clones the request into 2 requests, forwards them to 2 servers. The results from server A are returned as usual, but the results from server B are ignored.

teeproxy handles GET, POST, and all other http methods.

Build
-------------
```
go build
```

Usage
-------------
```
 ./teeproxy -l :8888 -a localhost:9000 -b localhost:9001
```
 `-l` specifies the listening port. `-a` and `-b` are meant for system A and B. The B system can be taken down or started up without causing any issue to the teeproxy.

#### Configuring timeouts ####
It's also possible to configure the timeout to both systems
*  `-a.timeout int`: timeout in seconds for production traffic (default `3`)
*  `-b.timeout int`: timeout in seconds for alternate site traffic (default `1`)

#### Configuring host header rewrite ####
Optionally rewrite host value in the http request header.
*  `-a.rewrite bool`: rewrite for production traffic (default `false`)
*  `-b.rewrite bool`: rewrite for alternate site traffic (default `false`)

#### Configuring a percentage of requests to alternate site ####
*  `-p float64`: only send a percentage of requests. The value is float64 for more precise control. (default `100.0`)

#### Configuring HTTPS ####
*  `-key.file string`: a TLS private key file. (default `""`)
*  `-cert.file string`: a TLS certificate file. (default `""`)

#### Configuring statsd ####
*  `-statsd.address string`: statsd server address in `host:port` format. (default `"localhost:8125"`)
*  `-statsd.prefix string`: a prefix to be used for your metrics. (default `"teeproxy"`)

#### Configuring logging ####
*  `-log.file string`: absolute path to the log file. (default `"/tmp/teeproxy.log"`)

NOTE: There is not support for log rotation as of this writing.

#### Sample script to start teeproxy ####
```
role=`grep role /etc/chef/first-boot.json | cut -d\[ -f3 | cut -d\] -f1 | tr '_' '-'`
host=`hostname -s`
statsd_prefix="redmart.$role.version.$host"
echo $statsd_prefix

sudo teeproxy -l :${LISTEN_PORT} -a ${PRIMARY_BACKEND} -b ${ALTERNATE_BACKEND} -a.timeout $PRIMARY_TIMEOUT -b.timeout ${ALTERNATE_TIMEOUT} -statsd.address ${STATSD_ADDRESS} -statsd.prefix $statsd_prefix -log.file /tmp/teeproxy.log
```

### TODO ###
* automated tests
* log rotation
* makefile & dependency management
