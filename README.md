## delete-unused-crossplane-crds

Crossplane offers a "ManagedResourceDefinition" CRD as a solution to the [CRD scaling problem](https://docs.crossplane.io/latest/managed-resources/managed-resource-definitions/#the-crd-scaling-problem).

For existing Crossplane installations, after setting up [Managed Resource Activation Policies](https://docs.crossplane.io/latest/managed-resources/managed-resource-activation-policies/) and updating Crossplane's [defaultActivations](https://docs.crossplane.io/latest/managed-resources/managed-resource-activation-policies/#activate-everything-default-behavior), you still need to manually clean up the CRD and MRD's that were installed by the providers.

In current kubectl context, this tool goes through all the _Managed Resource_'s, and deletes all corresponding CRD's that are unused and are not activated by any [MRAP](https://docs.crossplane.io/latest/managed-resources/managed-resource-activation-policies/).

### Install

```sh
go install github.com/nkzk/delete-unused-crossplane-crd@latest
```

### Usage

```bash
  -delete
    	DESTRUCTIVE: delete unused managed resource CRD's
  -dry-run
    	dry-run destructive operations. if true (default), --delete is not destructive (default true)
  -kubeconfig string
    	(optional) absolute path to the kubeconfig file
```

The user is prompted with a summary of number of unused MRD's marked for deletion and confirmation message.

### Delete default managed resource activation policy

Ref. [docs](https://docs.crossplane.io/latest/managed-resources/managed-resource-activation-policies/#activate-everything-default-behavior), existing installations will have a default MRAP that enables everything.

Delete it before proceeding, otherwise this tool will mark no MRD's for deletion.

#### dry-run delete

```sh
delete-unused-crossplane-crd  --delete
```

#### delete unused crds

```sh
delete-unused-crossplane-crd --delete --dry-run=false
```

### Note

Although destructive, this only deletes MRD and CRD's that are not in use, and are not activated by a _Managed Resource Activation Policy_

Proceed with caution and double-check the dry-run output anyways.

#### Bottleneck

A bottleneck in the current implemention is that a list call is sent to the kubernetes api server for each MRD to verify that it is not in use.

The operation may therefore take a few minutes to complete.
