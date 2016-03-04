package main

import (
  "fmt"
  "strconv"
  "bytes"
  "os"
  "text/template"
  "k8s.io/kubernetes/pkg/api"
  client "k8s.io/kubernetes/pkg/client/unversioned"
  "github.com/mholt/caddy/caddy"
)

var (
  AppName string = "CaddyIngress"
  AppVersion string = "v0.1"
)

// Keep a Caddyfile template in a constant
const (
  DefaultConfigFile = "/Caddyfile"
  templatesrc = `
{{range $vhost,$paths := . }}
http://{{$vhost}} {
{{range $path,$service := $paths }}
  proxy {{$path}} {{$service}} {
    proxy_header X-Real-IP {remote}
  }
{{end}}
}
{{end}}
`
)

type VHost map[string]string

type Router map[string]VHost

func getRouter (kubeClient *client.Client) Router {
  router := make(Router)
  ingressClient := kubeClient.Extensions().Ingress(api.NamespaceAll)
  ingresses, err := ingressClient.List(api.ListOptions{})
  if err != nil { panic(err) }
  for _,ingress := range ingresses.Items {
    for _,rule := range ingress.Spec.Rules {
      _, ok := router[rule.Host]
      if ok==false { router[rule.Host] = make(VHost) }
      for _,path := range rule.IngressRuleValue.HTTP.Paths {
        router[rule.Host][path.Path] = path.Backend.ServiceName+"."+ingress.Namespace+":"+strconv.Itoa(int(path.Backend.ServicePort.IntVal))
      }
    }
  }
  return router
}

func regenerateCaddyfile(router Router) []byte {
  tpl, err := template.New("test").Parse(templatesrc)
  if err != nil { panic(err) }
  var buffer bytes.Buffer
  err = tpl.Execute(&buffer, router)                                                                                                                                                                                                                            
  if err != nil { panic(err) }
  return buffer.Bytes()
}

func main() {
  kubeClient, err := client.NewInCluster()
  if err != nil { panic(err) }
  router := getRouter(kubeClient)
  watch, err := kubeClient.Extensions().Ingress(api.NamespaceAll).Watch(api.ListOptions{})
  if err != nil { panic(err) }  
  evts := watch.ResultChan()
  for {
    evt := <-evts
    if caddy.IsRestart() {
      fmt.Printf("Skip Caddy restart on evt : %v", evt)
      os.Setenv("CADDY_RESTART", "false")
    } else {
      fmt.Printf("Restart Caddy due to evt : %v", evt)
      err := caddy.Restart(caddy.CaddyfileInput{Contents: []byte(regenerateCaddyfile(router))})
      if err != nil { panic(err) }
//      caddy.Wait()
      os.Exit(0)
    }
  }

}
