## delete-unused-crossplane-crds

Crossplane offers a "ManagedResourceDefinition" CRD as a solution to the [CRD scaling problem](https://docs.crossplane.io/latest/managed-resources/managed-resource-definitions/#the-crd-scaling-problem).

For existing Crossplane installations, after setting up [Managed Resource Activation Policies](https://docs.crossplane.io/latest/managed-resources/managed-resource-activation-policies/) and updating Crossplane's [defaultActivations](https://docs.crossplane.io/latest/managed-resources/managed-resource-activation-policies/#activate-everything-default-behavior), you still need to manually clean up the CRDS that were installed by the providers.

In current kubectl context, this tool goes through all the *Managed Resource*'s, and deletes all corresponding CRD's that are unused and are not activated by any [MRAP](https://docs.crossplane.io/latest/managed-resources/managed-resource-activation-policies/).

### Usage

```sh
  -delete
    	DESTRUCTIVE: delete unused managed resource CRD's
  -dry-run
    	dry-run destructive operations. if true (default), --delete is not destructive (default true)
  -kubeconfig string
    	(optional) absolute path to the kubeconfig file 
```

#### dry-run delete
```sh
go run . --delete
```

#### [destructive] delete unused crds

```
go run . --delete --dry-run=false
```

### Note
Although destructive, this only deletes CRD's that are not in use.

Proceed with caution and double-check the dry-run output anyways.

