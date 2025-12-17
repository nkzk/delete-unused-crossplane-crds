package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	crossplanev1alpha1 "github.com/crossplane/crossplane/v2/apis/apiextensions/v1alpha1"
)

func main() {
	// get kubeconfig
	// ref https://github.com/kubernetes/client-go/blob/master/examples/dynamic-create-update-delete-deployment/main.go
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	var delete, dryRun *bool
	delete = flag.Bool("delete", false, "DESTRUCTIVE: delete unused managed resource CRD's")
	dryRun = flag.Bool("dry-run", true, "dry-run destructive operations. if true (default), --delete is not destructive")
	flag.Parse()

	if *delete == false && *dryRun == true {
		flag.Usage()
		os.Exit(0)
	}

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}
	config.QPS = 20
	config.Burst = 30

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	activated, err := getActiveMRDs(client)
	if err != nil {
		log.Panicf("failed to get Managed Resource Activation Policies: %v", err)
	}

	mrds, err := getMrds(client)
	if err != nil {
		log.Panicf("failed to get MRD's: %v", err)
	}

	for _, mrd := range mrds {
		for _, version := range mrd.Spec.Versions {
			gvr := schema.GroupVersionResource{
				Group:    mrd.Spec.Group,
				Resource: mrd.Spec.Names.Plural,
				Version:  version.Name,
			}

			l, err := list(client, gvr)
			if err != nil {
				log.Panicf("failed to list %v: %v", gvr, err)
			}

			if len(l.Items) > 0 {
				continue
			}

			if activated[mrd.Name] {
				continue
			}

			if *delete {
				name := mrd.GetName()
				if *dryRun {
					log.Printf("[dry-run] %s: number of usages: %d, deleting", name, len(l.Items))
					deleteGVR(client, schema.GroupVersionResource{
						Group:    "apiextensions.k8s.io",
						Version:  "v1",
						Resource: "customresourcedefinition",
					},
						name,
						true)
					if err != nil {
						log.Printf("failed to delete %s: %v", mrd.Spec.Names.Plural, err)
						continue
					}
				} else {
					log.Printf("deleting unused crossplane crd: %s\n", name)
					err := deleteGVR(client, schema.GroupVersionResource{
						Group:    "apiextensions.k8s.io",
						Version:  "v1",
						Resource: "customresourcedefinitions",
					},
						name,
						false)
					if err != nil {
						log.Printf("failed to delete %s: %v", name, err)
						continue
					}
				}
			}
		}
	}
}

func getActiveMRDs(client *dynamic.DynamicClient) (map[string]bool, error) {
	result := make(map[string]bool)

	l, err := list(client, schema.GroupVersionResource{
		Group:    "apiextensions.crossplane.io",
		Version:  "v1alpha1",
		Resource: "managedresourceactivationpolicies",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get managedresourceactivationpolicies: %v", err)
	}

	for _, item := range l.Items {
		mrap := crossplanev1alpha1.ManagedResourceActivationPolicy{}
		err := mapToType(item.UnstructuredContent(), &mrap)
		if err != nil {
			log.Panicf("failed to convert map to MRAP: %v", err)
		}

		for _, activated := range mrap.Status.Activated {
			result[activated] = true
		}
	}

	return result, nil
}

func getMrds(client *dynamic.DynamicClient) ([]crossplanev1alpha1.ManagedResourceDefinition, error) {
	gvr := schema.GroupVersionResource{
		Group:    "apiextensions.crossplane.io",
		Version:  "v1alpha1",
		Resource: "managedresourcedefinitions",
	}

	mrds, err := client.Resource(gvr).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var result []crossplanev1alpha1.ManagedResourceDefinition
	for _, mrd := range mrds.Items {
		crossplaneMrd := crossplanev1alpha1.ManagedResourceDefinition{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(mrd.UnstructuredContent(), &crossplaneMrd)
		if err != nil {
			log.Panicf("failed to convert unstructured %s mrd to to structured", mrd.GetName())
		}

		result = append(result, crossplaneMrd)
	}

	return result, nil
}

func mapToType[T any](source map[string]any, target *T) error {
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(source, target); err != nil {
		return err
	}
	return nil
}

func list(client *dynamic.DynamicClient, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error) {
	list, err := client.Resource(gvr).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get %v: %v\n", gvr, err)
	}

	return list, nil
}

func deleteGVR(client *dynamic.DynamicClient, gvr schema.GroupVersionResource, name string, dryRun bool) error {
	opts := metav1.DeleteOptions{}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	}

	return client.Resource(gvr).Delete(context.Background(), name, opts)
}
