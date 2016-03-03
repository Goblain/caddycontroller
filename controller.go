package main

import (
  "fmt"
//  "log"
  "os"
//  "os/exec"
//  "reflect"
  "text/template"
  "k8s.io/kubernetes/pkg/api"
//  "k8s.io/kubernetes/pkg/apis/extensions"
  client "k8s.io/kubernetes/pkg/client/unversioned"
//  "k8s.io/kubernetes/pkg/util"
)

// Keep a Caddyfile template in a constant
const (
  templatesrc = `
{{range $vhost,$paths := . }}
http://{{$vhost}} {
{{range $path,$endpoints := $paths }}
  proxy {{$path}} {{range $endpoint := $endpoints}}{{$endpoint}}{{end}} {
    proxy_header X-Real-IP {remote}
  }
{{end}}
}
{{end}}
`
)

type Endpoints map[string]string

type VHost map[string]Endpoints

type Router map[string]VHost

func getRouter () Router {
  kubeClient, err := client.NewInCluster()
//  services, err := kubeClient.Services(api.NamespaceAll).List(api.ListOptions{})
//  if err != nil { panic(err) }
  endpoints, err := kubeClient.Endpoints(api.NamespaceAll).List(api.ListOptions{})
  if err != nil { panic(err) }
  fmt.Printf("%v \n", endpoints)
  router := make(Router)
  var ingressClient client.IngressInterface
  if err != nil { panic(err) }
  ingressClient = kubeClient.Extensions().Ingress(api.NamespaceAll)
  ingresses, err := ingressClient.List(api.ListOptions{})
  for _,ingress := range ingresses.Items {
    for _,rule := range ingress.Spec.Rules {
      _, ok := router[rule.Host]
      if ok==false { router[rule.Host] = make(VHost) }
      for _,path := range rule.IngressRuleValue.HTTP.Paths {
        router[rule.Host][path.Path] = make(Endpoints)
//        fmt.Printf("Path: %v \n", path.Path)
//        fmt.Printf("ServiceName: %v \n", path.Backend.ServiceName)
//        fmt.Printf("ServicePort: %v \n", path.Backend.ServicePort.IntVal)
      }
    }
  }
  return router
}

func main() {
  router := getRouter()
  tpl, err := template.New("test").Parse(templatesrc)
  if err != nil { panic(err) }
  err = tpl.Execute(os.Stdout, router)
}
