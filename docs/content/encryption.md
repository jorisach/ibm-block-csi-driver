# Volume Encryption with LUKS

## Overview

The IBM Block CSI Driver supports volume encryption using LUKS (Linux Unified Key Setup). When enabled, volumes are encrypted at the host level using dm-crypt, providing an additional layer of security for data at rest.

## Features

- **Transparent encryption**: Encryption is handled automatically by the CSI driver
- **AES-XTS encryption**: Industry-standard encryption cipher with hardware acceleration support
- **LUKS2 format**: Modern LUKS format with enhanced security features
- **Key management**: Encryption passphrases stored securely in Kubernetes Secrets
- **Volume expansion**: Encrypted volumes can be expanded seamlessly
- **Backward compatible**: Encryption is opt-in via StorageClass parameters

## How It Works

1. **Volume Creation**: When a PVC is created with an encrypted StorageClass, the controller provisions the volume normally on the storage array
2. **Volume Staging**: During NodeStageVolume, the node component:
   - Retrieves the encryption passphrase from the specified Kubernetes Secret
   - Formats the device with LUKS (if not already formatted)
   - Opens the LUKS device to create a mapper device at `/dev/mapper/ibm_<volumeID>`
   - Formats and mounts the mapper device
3. **Volume Expansion**: During resize operations, both the LUKS layer and filesystem are expanded
4. **Volume Cleanup**: During NodeUnstageVolume, the LUKS device is closed and the multipath device is cleaned up

## Prerequisites

### Node Requirements

1. **cryptsetup package**: The `cryptsetup` tool must be available on all nodes
   ```bash
   # Check if cryptsetup is installed
   which cryptsetup
   
   # Install on Ubuntu/Debian
   apt-get install cryptsetup
   
   # Install on RHEL/CentOS
   yum install cryptsetup
   ```

2. **dm-crypt kernel module**: Usually built-in on modern Linux distributions
   ```bash
   # Verify dm-crypt is available
   modprobe dm-crypt
   lsmod | grep dm_crypt
   ```

### RBAC Permissions

The node ServiceAccount needs permission to read Secrets:

```bash
kubectl apply -f deploy/kubernetes/examples/rbac-encryption.yaml
```

## Setup Guide

### Step 1: Create an Encryption Secret

Create a Kubernetes Secret containing the LUKS passphrase:

```bash
# Create secret from literal
kubectl create secret generic luks-passphrase \
  --from-literal=encryptionPassphrase=YourStrongPasswordHere \
  -n kube-system

# Or apply from YAML
kubectl apply -f deploy/kubernetes/examples/demo-encryption-secret.yaml
```

**Important**: 
- Use a strong, randomly generated passphrase (minimum 20 characters recommended)
- Store the passphrase securely in a password manager or secrets vault
- **Loss of the passphrase means permanent data loss**

### Step 2: Create an Encrypted StorageClass

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ibm-block-encrypted
provisioner: block.csi.ibm.com
parameters:
  pool: "pool1"
  SpaceEfficiency: "thin"
  
  # Encryption parameters
  encrypted: "true"
  encryptionSecret: "luks-passphrase"
  encryptionSecretNamespace: "kube-system"
  
  # Array credentials (required)
  csi.storage.k8s.io/provisioner-secret-name: "ibm-block-csi-driver"
  csi.storage.k8s.io/provisioner-secret-namespace: "kube-system"
  csi.storage.k8s.io/controller-publish-secret-name: "ibm-block-csi-driver"
  csi.storage.k8s.io/controller-publish-secret-namespace: "kube-system"

volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
reclaimPolicy: Delete
```

Apply the StorageClass:
```bash
kubectl apply -f deploy/kubernetes/examples/demo-storageclass-encrypted.yaml
```

### Step 3: Create an Encrypted PVC

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-encrypted-pvc
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  storageClassName: ibm-block-encrypted
```

Apply the PVC:
```bash
kubectl apply -f deploy/kubernetes/examples/demo-pvc-encrypted.yaml
```

### Step 4: Use the PVC in a Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: app-with-encrypted-storage
spec:
  containers:
  - name: app
    image: nginx
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: my-encrypted-pvc
```

## Verification

### Verify LUKS Formatting

On a node where the volume is mounted:

```bash
# Find the multipath device
volumeId="a9000:1234567:600507123456789"  # From PV
mpathDevice=$(multipath -l | grep ${volumeId} | awk '{print $1}')

# Check if device is LUKS formatted
cryptsetup isLuks /dev/mapper/${mpathDevice}
echo $?  # Should return 0 for LUKS devices

# View LUKS header information
cryptsetup luksDump /dev/mapper/${mpathDevice}
```

### Verify LUKS Mapper Device

```bash
# List active LUKS mappers
ls -l /dev/mapper/ibm_*

# Check mapper status
volumeId="a9000:1234567:600507123456789"
sanitizedId=$(echo $volumeId | tr ':' '_')
cryptsetup status ibm_${sanitizedId}
```

### Verify Device Stack

```bash
# Show device hierarchy
lsblk -f

# Example output for encrypted volume:
# NAME                              FSTYPE      LABEL UUID                                 MOUNTPOINT
# sda                                                                                      
# └─mpatha (dm-0)                   crypto_LUKS       12345678-1234-1234-1234-123456789012 
#   └─ibm_a9000_1234567_600507... (dm-1) ext4            abcdef01-2345-6789-abcd-ef0123456789 /var/lib/kubelet/...
```

## Configuration

### StorageClass Parameters

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `encrypted` | No | `false` | Enable LUKS encryption for volumes |
| `encryptionSecret` | Yes (if encrypted=true) | - | Name of the Secret containing the passphrase |
| `encryptionSecretNamespace` | Yes (if encrypted=true) | - | Namespace of the encryption Secret |

### Secret Format

The encryption Secret must contain a key named `encryptionPassphrase`:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: luks-passphrase
  namespace: kube-system
type: Opaque
data:
  encryptionPassphrase: <base64-encoded-passphrase>
```

## Security Considerations

### Passphrase Management

1. **Use strong passphrases**: Minimum 20 characters, randomly generated
2. **Secure storage**: Store passphrases in a secure vault (e.g., HashiCorp Vault, AWS Secrets Manager)
3. **Backup**: Maintain secure backups of passphrases - loss means data loss
4. **Rotation**: Consider periodic passphrase rotation (requires manual steps)
5. **Access control**: Limit Secret access with RBAC

### Scope of Protection

- **Protects against**: Physical disk theft, unauthorized access to storage array snapshots, decommissioned disks
- **Does NOT protect against**: 
  - Root access on the node (root can read mounted filesystems)
  - Kubernetes RBAC compromise (attacker with Secret access)
  - Memory dumps while volume is mounted

### Multi-Tenancy

- Different namespaces can use different encryption secrets
- StorageClass can specify namespace-specific secrets
- Node requires cluster-wide Secret read permission (CSI requirement)

## Performance

### Overhead

- **CPU**: <5% with AES-NI hardware acceleration (available on modern Intel/AMD CPUs)
- **Throughput**: Minimal impact with hardware acceleration
- **Latency**: <1ms additional latency
- **Format time**: <5 seconds (only LUKS header, not full disk)

### Hardware Acceleration

Check if AES-NI is available:
```bash
grep -m1 -o aes /proc/cpuinfo
# Should output: aes
```

## Limitations

1. **New volumes only**: Encryption can only be applied to new, empty volumes
   - **Safety Protection**: The driver automatically detects non-empty devices and refuses to encrypt them to prevent data loss
   - If a volume contains existing data (filesystem or partition table), encryption will fail with error: `FailedPrecondition: cannot encrypt volume: device contains existing data`
2. **No retroactive encryption**: Existing volumes cannot be encrypted in place
3. **No passphrase rotation**: Changing passphrases requires data migration
4. **Single passphrase per volume**: One Secret per StorageClass

## Troubleshooting

### Volume Fails to Mount

**Symptom**: Pod stuck in `ContainerCreating`, events show volume attach/mount failure

**Checks**:
1. Verify cryptsetup is installed on node:
   ```bash
   kubectl debug node/<node-name> -it --image=busybox
   chroot /host
   which cryptsetup
   ```

2. Check node logs for LUKS errors:
   ```bash
   kubectl logs -n kube-system ibm-block-csi-node-<pod> -c ibm-block-csi-node
   ```

3. Verify Secret exists and is readable:
   ```bash
   kubectl get secret luks-passphrase -n kube-system
   ```

4. Check RBAC permissions:
   ```bash
   kubectl auth can-i get secrets --as=system:serviceaccount:kube-system:ibm-block-csi-node
   ```

### Passphrase Not Found Error

**Error**: `failed to retrieve passphrase: key 'encryptionPassphrase' not found in secret`

**Solution**: Ensure Secret has the correct key name:
```bash
kubectl get secret luks-passphrase -n kube-system -o jsonpath='{.data}'
# Should show: {"encryptionPassphrase":"..."}
```

### Volume Expansion Fails

**Symptom**: PVC resize fails for encrypted volumes

**Checks**:
1. Verify LUKS mapper is active:
   ```bash
   cryptsetup status ibm_<volumeId>
   ```

2. Check node logs for LUKS resize errors:
   ```bash
   kubectl logs -n kube-system ibm-block-csi-node-<pod> -c ibm-block-csi-node | grep -i resize
   ```

### LUKS Device Not Closing on Unstage

**Symptom**: Mapper device remains after pod deletion

**Solution**: Cleanup manually if needed:
```bash
# Find the mapper
ls /dev/mapper/ibm_*

# Close LUKS device
cryptsetup luksClose ibm_<volumeId>

# Flush multipath
multipath -f /dev/mapper/<mpath-device>
```

### Cannot Encrypt Volume - Device Contains Data

**Error**: `FailedPrecondition: cannot encrypt volume: device contains existing data`

**Cause**: You are attempting to encrypt a volume that already contains a filesystem or data. This is a **safety feature** to prevent accidental data loss.

**Solutions**:

1. **For new PVCs**: Ensure you're using the encrypted StorageClass from the beginning
   ```yaml
   apiVersion: v1
   kind: PersistentVolumeClaim
   metadata:
     name: new-encrypted-pvc
   spec:
     storageClassName: ibm-block-encrypted  # Use encrypted SC from start
     accessModes: [ReadWriteOnce]
     resources:
       requests:
         storage: 10Gi
   ```

2. **For existing data**: Migrate to a new encrypted volume:
   ```bash
   # Create new encrypted PVC
   kubectl apply -f encrypted-pvc.yaml
   
   # Use a migration pod to copy data
   kubectl run migrator --rm -it --image=ubuntu \
     --overrides='{"spec":{"volumes":[
       {"name":"old","persistentVolumeClaim":{"claimName":"old-pvc"}},
       {"name":"new","persistentVolumeClaim":{"claimName":"new-encrypted-pvc"}}
     ],"containers":[{"name":"migrator","image":"ubuntu",
       "command":["bash"],
       "volumeMounts":[
         {"name":"old","mountPath":"/old"},
         {"name":"new","mountPath":"/new"}
       ]}]}}'
   
   # Inside pod:
   apt update && apt install -y rsync
   rsync -avP /old/ /new/
   exit
   
   # Update your application to use new-encrypted-pvc
   # Delete old PVC when confirmed working
   ```

3. **Verification**: Check device state before encryption
   ```bash
   # On node, check if device has data
   blkid -p /dev/mapper/mpathX
   
   # No output = empty device (safe to encrypt)
   # Output with TYPE=... = device has data (cannot encrypt)
   ```

## Backup and Disaster Recovery

### Backing Up Encrypted Volumes

1. **Volume snapshots**: CSI snapshots capture the encrypted LUKS container
2. **Restore requirements**: Same passphrase required for restore
3. **Passphrase backup**: Store passphrases in secure vault alongside backups

### Disaster Recovery Scenarios

#### Lost Passphrase
- **Result**: Permanent data loss
- **Prevention**: Maintain secure backups of encryption secrets
- **Recovery**: None - data is unrecoverable

#### Node Failure
- Volume automatically re-attaches to new node
- LUKS device reopened with passphrase from Secret

#### Secret Deletion
- Existing mounted volumes continue to work
- New mounts fail until Secret is restored

## Examples

### Multiple StorageClasses with Different Keys

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: team-a-encryption
  namespace: team-a
type: Opaque
data:
  encryptionPassphrase: <team-a-passphrase>
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: team-a-encrypted
provisioner: block.csi.ibm.com
parameters:
  pool: "pool1"
  encrypted: "true"
  encryptionSecret: "team-a-encryption"
  encryptionSecretNamespace: "team-a"
  # ... other parameters
```

### Using with StatefulSets

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: database
spec:
  serviceName: database
  replicas: 3
  selector:
    matchLabels:
      app: database
  template:
    metadata:
      labels:
        app: database
    spec:
      containers:
      - name: postgres
        image: postgres:14
        volumeMounts:
        - name: data
          mountPath: /var/lib/postgresql/data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      storageClassName: ibm-block-encrypted
      resources:
        requests:
          storage: 50Gi
```

## References

- [LUKS Specification](https://gitlab.com/cryptsetup/cryptsetup)
- [dm-crypt Documentation](https://www.kernel.org/doc/html/latest/admin-guide/device-mapper/dm-crypt.html)
- [Kubernetes Secrets](https://kubernetes.io/docs/concepts/configuration/secret/)
