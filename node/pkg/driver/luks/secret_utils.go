/**
 * Copyright 2019 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package luks

import (
	"context"
	"fmt"

	"github.com/ibm/ibm-block-csi-driver/node/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// EncryptionPassphraseKey is the key name for the encryption passphrase in Kubernetes Secrets
	EncryptionPassphraseKey = "encryptionPassphrase"
)

// GetEncryptionPassphrase retrieves the encryption passphrase from a Kubernetes Secret
func GetEncryptionPassphrase(ctx context.Context, secretName, secretNamespace string) (string, error) {
	logger.Infof("Retrieving encryption passphrase from secret %s/%s", secretNamespace, secretName)

	if secretName == "" || secretNamespace == "" {
		return "", &PassphraseRetrievalError{
			SecretName:      secretName,
			SecretNamespace: secretNamespace,
			Err:             fmt.Errorf("secret name or namespace is empty"),
		}
	}

	// Create in-cluster Kubernetes client
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return "", &PassphraseRetrievalError{
			SecretName:      secretName,
			SecretNamespace: secretNamespace,
			Err:             fmt.Errorf("unable to load in-cluster configuration: %v", err),
		}
	}

	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return "", &PassphraseRetrievalError{
			SecretName:      secretName,
			SecretNamespace: secretNamespace,
			Err:             fmt.Errorf("unable to create Kubernetes client: %v", err),
		}
	}

	// Retrieve the secret
	secret, err := client.CoreV1().Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return "", &PassphraseRetrievalError{
			SecretName:      secretName,
			SecretNamespace: secretNamespace,
			Err:             fmt.Errorf("unable to get secret: %v", err),
		}
	}

	// Extract passphrase from secret data
	passphraseBytes, ok := secret.Data[EncryptionPassphraseKey]
	if !ok {
		return "", &PassphraseRetrievalError{
			SecretName:      secretName,
			SecretNamespace: secretNamespace,
			Err:             fmt.Errorf("key '%s' not found in secret", EncryptionPassphraseKey),
		}
	}

	if len(passphraseBytes) == 0 {
		return "", &PassphraseRetrievalError{
			SecretName:      secretName,
			SecretNamespace: secretNamespace,
			Err:             fmt.Errorf("passphrase is empty"),
		}
	}

	logger.Infof("Successfully retrieved encryption passphrase from secret %s/%s", secretNamespace, secretName)
	return string(passphraseBytes), nil
}

// ClearPassphrase securely clears a passphrase from memory
func ClearPassphrase(passphrase *string) {
	if passphrase != nil && *passphrase != "" {
		// Overwrite the string memory with zeros
		b := []byte(*passphrase)
		for i := range b {
			b[i] = 0
		}
		*passphrase = ""
	}
}
