# DNS proxy with ad blocker

This is a high performance DNS proxy with blacklist and whitelist feature to fight unwanted ads, also works for filtering unwanted domain when accessing your internet.

This project is written in Go, heavily inspired by [looterz/grimd](https://github.com/looterz/grimd), and with helps of these great libraries:

- [miekg/dns](https://github.com/miekg/dns) - Alternative (more granular) approach to a DNS library
- [gobwas/glob](https://github.com/gobwas/glob) - Go Globbing Library
- [boltdb/bolt](https://github.com/boltdb/bolt) - Bolt, a pure Go key/value store database
- [joyrexus/buckets](https://github.com/joyrexus/buckets) - a Bolt wrapper streamlining simple tx and key scans

## Installation

```console
$ go get -v github.com/frengky/adblockr/...
```
This command will install `adblockr` binary into your `GOPATH/bin`

## Configuration
First, create a configuration file `adblockr.yml` to getting started
```yml
# Bind DNS service to address  
listen_address: "127.0.0.1:5300"  
  
# List of upstream nameservers to be contacted when passed our blacklist check  
nameservers:  
#  - "172.16.8.8:53"  
  - "1.1.1.1:53"  
  - "8.8.8.8:53"  
  
# List of blacklist source uri, format: https://some/blacklist.txt or file:///local/path/file.txt  
blacklist_sources:  
  - https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts
  - https://mirror1.malwaredomains.com/files/justdomains
  
# List of whitelisted domains, format: some.domain.com or *.domain.com  
whitelist_domains:  
  - "www.googleadservices.com"  
  
# Location of database file, if empty all blacklist will be stored on memory instead  
db_file: adblockr.db
```

## Quick start

Running the DNS proxy verbosely with a configuration file:
```console
$ adblockr serve -v -c /path/to/adblockr.yml
```
A database contains blacklisted domain `adblockr.db` will be created when its running for the first time, or you can also manually initialize the database (downloading all blacklist sources from `adblockr.yml`) using this command:
```console
$ adblockr init-db
```
> The `adblockr.db` blacklist database file will be created in the current working directory. 
> Please only initialize the database when server is **not** running.

for more available commands, please see `adblockr --help`

## Tips

This DNS proxy server is created mainly for blacklisting/whitelisting domain purposes, so it does not cache at all. For more privacy and protections, i would recommend to run this server together with [unbound](https://github.com/NLnetLabs/unbound) as upstream caching DNS resolver that fully support **DNS over HTTPS** and **DNS over TLS** (DoH/DoT).