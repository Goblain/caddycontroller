package main

import (
  "syscall"
  "time"
  "fmt"
  "io/ioutil"
  "strconv"
  "bytes"
  "os/exec"
  "text/template"
  "k8s.io/kubernetes/pkg/api"
  client "k8s.io/kubernetes/pkg/client/unversioned"
)

// Keep a Caddyfile template in a constant
const (
  DefaultConfigFile = "/Caddyfile"
  templatesrc = `
{{range $vhost,$paths := . }}
http://{{$vhost}}:80 {
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

func regenerateCaddyfile(router Router) {
  tpl, err := template.New("test").Parse(templatesrc)
  if err != nil { panic(err) }
  var buffer bytes.Buffer
  err = tpl.Execute(&buffer, router)
  if err != nil { panic(err) }
  fmt.Printf("Generated Caddyfile content :\n %v \n\n", buffer.String())
  ioutil.WriteFile("/Caddyfile", buffer.Bytes(), 644)
}

func launchCaddy() {
  cmd := exec.Command("/caddy", "--pidfile", "/caddy.pid", "--conf", "/Caddyfile")
  if err := cmd.Run(); err != nil {
    if err.Error() == "exit status 1" && cmd.Process.Pid != getCaddyPid() {
      fmt.Printf("Parent process exited with child alive")
    } else {
      panic(err)
    }
  }
}

func getCaddyPid() int {
  filebytes, err := ioutil.ReadFile("/caddy.pid")
  if err != nil { panic(err) }
  pid := bytes.NewBuffer(filebytes).String()
  intpid, err := strconv.Atoi(pid)
  return intpid
}

func reloadCaddy() {
  fmt.Printf("Reload caddy pid: %v", getCaddyPid())
  syscall.Kill(getCaddyPid(), 10) 
}

func main() {
  kubeClient, err := client.NewInCluster()
  if err != nil { panic(err) }
  regenerateCaddyfile(getRouter(kubeClient))
  watch, err := kubeClient.Extensions().Ingress(api.NamespaceAll).Watch(api.ListOptions{})
  if err != nil { panic(err) }
  go launchCaddy()
  time.Sleep(time.Second)
  evts := watch.ResultChan()
  for {
    evt := <-evts
    fmt.Printf("Restart Caddy due to evt : %v", evt)
    regenerateCaddyfile(getRouter(kubeClient))
    reloadCaddy()
  }
}
