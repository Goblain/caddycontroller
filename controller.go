package main

import (
  "reflect"
  "syscall"
  "time"
  "fmt"
  "github.com/golang/glog"
  "io/ioutil"
  "strconv"
  "bytes"
  "os"
  "os/exec"
  "os/signal"
  "text/template"
  "k8s.io/kubernetes/pkg/api"
  "k8s.io/kubernetes/pkg/controller/framework"
  "k8s.io/kubernetes/pkg/apis/extensions"
  "k8s.io/kubernetes/pkg/client/cache"
  "k8s.io/kubernetes/pkg/runtime"
  "k8s.io/kubernetes/pkg/watch"

  client "k8s.io/kubernetes/pkg/client/unversioned"
)

// Keep a Caddyfile template in a constant
const (
  DefaultConfigFile = "/Caddyfile"
  templatesrc = `
{{range $vhost,$paths := . }}
http://{{$vhost}}:80 {
  tls off
  log stdout
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
  if err != nil { return -1 }
  pid := bytes.NewBuffer(filebytes).String()
  intpid, err := strconv.Atoi(pid)
  return intpid
}

func reloadCaddy() {
  fmt.Printf("Reload caddy pid: %v", getCaddyPid())
  pid := getCaddyPid()
  if pid > 0 { 
    syscall.Kill(pid, 10) 
  } else {
    launchCaddy()
  }  
}


// CaddyController is intended to launch as root process of a contaier in K8S POD, 
// aS such, it faces PID1 zombie reaping responsibility so reaping function is created below
// (code borrowed from OpenShift)
func grimmReaper() {
// that has pid 1.
  if os.Getpid() == 1 {
    go func() {
      sigs := make(chan os.Signal, 1)
      signal.Notify(sigs, syscall.SIGCHLD)
      for {
        // Wait for a child to terminate
        sig := <-sigs
        glog.Infof("Signal recieved : %v", sig)
        for {
          // Reap processes
          _, err := syscall.Wait4(-1, nil, 0, nil)
          // Break out if there are no more processes to read
          if err == syscall.ECHILD {
            break
          }
        }
      }
    }()
  }
}

func getIngressNotificationChannel(c *client.Client) (chan interface{}) {
  // Create channel to send notifications about changed ingress objects
  notifications := make(chan interface{})

  // define handlers for Add/Delete/Update to notify with changed object
  handlers := framework.ResourceEventHandlerFuncs{
    AddFunc: func(obj interface{}) { glog.Info("Handler AddFunc"); notifications <- obj },
    DeleteFunc: func(obj interface{}) { glog.Info("Handler DeleteFunc");  notifications <- obj },
    UpdateFunc: func(old, cur interface{}) { glog.Info("Handler UpdateFunc"); if !reflect.DeepEqual(old, cur) { notifications <- cur } } }

  var ingressController *framework.Controller
  // Create informer that will watch for ingress changes and trigger handlers
  _, ingressController = framework.NewInformer(
    &cache.ListWatch{
      ListFunc:  func(api.ListOptions) (runtime.Object, error) { return c.Extensions().Ingress(api.NamespaceAll).List(api.ListOptions{}) },
      WatchFunc: func(api.ListOptions) (watch.Interface, error) { return c.Extensions().Ingress(api.NamespaceAll).Watch(api.ListOptions{}) } },
    &extensions.Ingress{}, time.Minute, handlers)

  stopChannel := make(chan struct {})
  go ingressController.Run(stopChannel)

  return notifications
}

func main() {
  grimmReaper()
  kubeClient, err := client.NewInCluster()
  if err != nil { panic(err) }
  regenerateCaddyfile(getRouter(kubeClient))
  launchCaddy()
  time.Sleep(time.Second)
  glog.Info("Get notification channel from watchers")
  evts := getIngressNotificationChannel(kubeClient)
  for {
    glog.Info("EventLoop")
    evt := <-evts
    glog.Infof("Restart Caddy due to evt : %v", evt)
    regenerateCaddyfile(getRouter(kubeClient))
    reloadCaddy()
  }
}
