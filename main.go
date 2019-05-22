package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	extv1beta1 "k8s.io/api/extensions/v1beta1"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/gobwas/glob"
	"github.com/spf13/pflag"
)

const (
	ingressClassAnnotation      = "kubernetes.io/ingress.class"
	kongConfigurationAnnotation = "configuration.konghq.com"
)

var (
	ingressClass    string
	ingressPattern  glob.Glob
	kongIngressName string
)

func init() {
	pflag.StringVar(&kongIngressName, "kong-ingress-name", "cert-manager-http01", "Name of KongIngress to add")
	pflag.StringVar(&ingressClass, "ingress-class", "kong", "Ingress class being routed through Kong")
	pattern := pflag.String("ingress-pattern", "cm-acme-http-solver-*", "Glob pattern for ingress name")
	pflag.Parse()
	ingressPattern = glob.MustCompile(*pattern)
}


func main() {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	factory := informers.NewSharedInformerFactory(clientset, 2*time.Minute)

	ingressInformer := factory.Extensions().V1beta1().Ingresses().Informer()

	stopper := make(chan struct{})
	defer close(stopper)

	ingressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ingress := obj.(*extv1beta1.Ingress)
			class, hasAnnotation := ingress.Annotations[ingressClassAnnotation]
			ingressName, hasKongAnnotation := ingress.Annotations[kongConfigurationAnnotation]
			if ingressPattern.Match(ingress.GetName()) && hasAnnotation && class == ingressClass {
				log.Printf("Matching ingress added: %s", ingress.GetName())
				for _, rule := range ingress.Spec.Rules {
					for _, path := range rule.IngressRuleValue.HTTP.Paths {
						log.Printf("  path %s\n", path.Path)
					}
				}

				if !hasKongAnnotation || ingressName != kongIngressName {
					ingress.Annotations[kongConfigurationAnnotation] = kongIngressName

					updatedIngress, err := clientset.Extensions().Ingresses(ingress.GetNamespace()).Update(ingress)

					if err != nil {
						log.Printf("failed to patch ingress: %s\n", err.Error())
					} else {
						log.Printf("successfully patched ingress: %s\n", updatedIngress.GetName())
					}
				}
			}
		},
	})
	go ingressInformer.Run(stopper)

	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGTERM)
	signal.Notify(sigterm, syscall.SIGINT)
	<-sigterm
}
