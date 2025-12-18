package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	crossplanev1alpha1 "github.com/crossplane/crossplane/v2/apis/apiextensions/v1alpha1"
)

type CRD struct {
	schema.GroupVersionResource
	Name string
}

type MRD struct {
	schema.GroupVersionResource
	Name string
	Crd  *CRD
}

var (
	mrdGVR = schema.GroupVersionResource{
		Group:    "apiextensions.crossplane.io",
		Version:  "v1alpha1",
		Resource: "managedresourcedefinitions",
	}

	crdGVR = schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}
	mrapGVR = schema.GroupVersionResource{
		Group:    "apiextensions.crossplane.io",
		Version:  "v1alpha1",
		Resource: "managedresourceactivationpolicies",
	}
)

func main() {
	// get kubeconfig
	// ref https://github.com/kubernetes/client-go/blob/master/examples/dynamic-create-update-delete-deployment/main.go
	var kubeconfig *string
	var delete, dryRun *bool

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	delete = flag.Bool("delete", false, "DESTRUCTIVE: delete unused managed resource CRD's")
	dryRun = flag.Bool("dry-run", true, "dry-run destructive operations. if true (default), --delete is not destructive")

	flag.Parse()

	if *delete == false && *dryRun == true {
		flag.Usage()
		os.Exit(0)
	}

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("failed to build kubeconfig: %v", err)
	}

	config.QPS = 80
	config.Burst = 100

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("failed to create kube client: %v", err)
	}

	log.Printf("getting active MRD's from activation policies")
	activated, err := getActiveMRDs(client)
	if err != nil {
		log.Fatalf("failed to get Managed Resource Activation Policies: %v", err)
	}

	log.Printf("getting all MRD's in cluster")
	allMrds, err := getMrds(client)
	if err != nil {
		log.Fatalf("failed to get MRD's: %v", err)
	}

	log.Printf("getting all CRD's in cluster")
	allCrds, err := list(client, crdGVR)
	if err != nil {
		log.Fatalf("failed to list all CRDs: %v", err)
	}

	// o(1) crd lookup instead of linear (number of mrds * number of crds)
	crdMap := make(map[string]unstructured.Unstructured, 0)
	for _, crd := range allCrds.Items {
		crdMap[string(crd.GetName())] = crd
	}

	unusedMrds := make([]MRD, 0)

	// fill unused MRDs array
	// if MRD is in use, is activated by activation policy, or corresponding CRD does not exist - skip
	log.Printf("building overview of unused MRD and CRD's, this may take some time...")
	for _, mrd := range allMrds {
		for _, version := range mrd.Spec.Versions {
			gvr := schema.GroupVersionResource{
				Group:    mrd.Spec.Group,
				Resource: mrd.Spec.Names.Plural,
				Version:  version.Name,
			}

			// bottleneck
			l, err := list(client, gvr)
			if err != nil {
				log.Fatalf("failed to list %v: %v", gvr, err)
			}

			// mrd is unused
			if len(l.Items) > 0 {
				continue
			}

			// mrd is unused but activated by MRAP
			if activated[mrd.Name] {
				continue
			}

			var unstructuredCrd unstructured.Unstructured

			unstructuredCrd, ok := crdMap[mrd.Name]
			if !ok {
				continue
			}

			// if the MRD is in the CRD's ownereferences, append
			for _, owner := range unstructuredCrd.GetOwnerReferences() {
				if string(owner.UID) == string(mrd.UID) {
					unusedMrds = append(unusedMrds, MRD{
						Crd: &CRD{
							Name:                 mrd.Name,
							GroupVersionResource: crdGVR,
						},
						GroupVersionResource: mrdGVR,
						Name:                 mrd.Name,
					})
				}
			}
		}
	}

	log.Printf("unused crossplane MRD and corresponding CRD's marked for deletion:")
	for _, mrd := range unusedMrds {
		fmt.Printf("\t%s\n", mrd.Name)
	}

	log.Printf("total MRDs: %d, unused MRDs selected for deletion: %d", total(allMrds), len(unusedMrds))
	log.Printf("dry-run: %v", *dryRun)

	yes := confirm("continue")
	if yes {
		del(client, delete, dryRun, unusedMrds)
		log.Printf("done")
	}
}

func del(client *dynamic.DynamicClient, delete, dryRun *bool, mrds []MRD) {
	for _, mrd := range mrds {
		if *delete {
			err := deleteGVR(client,
				mrd.GroupVersionResource,
				mrd.Name,
				*dryRun)
			if err != nil {
				log.Printf("failed to delete %s: %v", mrd.GroupVersionResource.Resource, err)
				continue
			}

			if mrd.Crd != nil {
				err = deleteGVR(client,
					crdGVR,
					mrd.Crd.Name,
					*dryRun)
				if err != nil {
					log.Printf("failed to delete %s: %v", mrd.GroupVersionResource.Resource, err)
					continue
				}
			}
		}
	}
}

func getActiveMRDs(client *dynamic.DynamicClient) (map[string]bool, error) {
	result := make(map[string]bool)

	l, err := list(client, mrapGVR)
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
	mrds, err := client.Resource(mrdGVR).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var result []crossplanev1alpha1.ManagedResourceDefinition
	for _, mrd := range mrds.Items {
		crossplaneMrd := crossplanev1alpha1.ManagedResourceDefinition{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(mrd.UnstructuredContent(), &crossplaneMrd)
		if err != nil {
			return result, fmt.Errorf("failed to convert unstructured %s mrd to to structured", mrd.GetName())
		}

		result = append(result, crossplaneMrd)
	}

	return result, nil
}

func mapToType[T any](source map[string]any, target *T) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(source, target)
}

func get(client *dynamic.DynamicClient, gvr schema.GroupVersionResource, name string) (*unstructured.Unstructured, error) {
	return client.Resource(gvr).Get(context.Background(), name, metav1.GetOptions{})
}

// list all occurences of GVR
func list(client *dynamic.DynamicClient, gvr schema.GroupVersionResource) (*unstructured.UnstructuredList, error) {
	list, err := client.Resource(gvr).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get %v: %v\n", gvr, err)
	}

	return list, nil
}

// delete
func deleteGVR(client *dynamic.DynamicClient, gvr schema.GroupVersionResource, name string, dryRun bool) error {
	opts := metav1.DeleteOptions{}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	}

	return client.Resource(gvr).Delete(context.Background(), name, opts)
}

// wait for user confirmation
func confirm(s string) bool {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("%s [y/n]: ", s)

		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}

		response = strings.ToLower(strings.TrimSpace(response))

		switch response {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
}

// count total mrds, including different versions
func total(mrds []crossplanev1alpha1.ManagedResourceDefinition) int {
	count := 0
	for _, mrd := range mrds {
		for range mrd.Spec.Versions {
			count++
		}
	}

	return count
}
