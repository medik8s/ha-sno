package controllers

import (
	"context"
	"fmt"
	"github.com/mshitrit/hasno-setup-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

const (
	defaultNamespace       = "default"
	haLayerCRName          = "test"
	node1Name              = "mock-cluster1-node"
	node2Name              = "mock-cluster2-node"
	originalFenceAgentName = "mock-fence-org"
	fenceAgentType         = "fence_mock"
	updatedFenceAgentName  = "mock-fence-changed"
)

var (
	haLayerPodName = createPodTemplateNameFromCRName(haLayerCRName)
)

type userAction int

const (
	doNothing userAction = iota
	createCR
	deleteCR
	updateCR
)

var _ = Describe("High Availability Layer Set CR", func() {
	var (
		underTest  *v1alpha1.HALayerSet
		userAction userAction
	)

	Context("Defaults", func() {
		BeforeEach(func() {
			underTest = &v1alpha1.HALayerSet{
				ObjectMeta: metav1.ObjectMeta{Name: haLayerCRName, Namespace: defaultNamespace},
				Spec:       v1alpha1.HALayerSetSpec{FenceAgentsSpec: []v1alpha1.FenceAgentSpec{{Params: map[string]string{}}}},
			}
			err := k8sClient.Create(context.Background(), underTest)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			err := k8sClient.Delete(context.Background(), underTest)
			Expect(err).NotTo(HaveOccurred())
		})

		When("creating a resource", func() {
			It("CR is namespace scoped", func() {
				Expect(underTest.Namespace).To(Not(BeEmpty()))
			})

		})
	})

	Context("Reconciliation", func() {

		BeforeEach(func() {
			underTest = createHALayerSetCR()

		})
		JustBeforeEach(func() {
			switch userAction {
			case createCR:
				Expect(k8sClient.Create(context.Background(), underTest)).ToNot(HaveOccurred())
			case deleteCR:
				Expect(k8sClient.Delete(context.Background(), underTest)).ToNot(HaveOccurred())
				verifyCrIsMissing()
				verifyHADeploymentIsMissing()
				verifyHAPodIsMissing()
			case updateCR:
				existingCR, err := getHALayerCR()
				Expect(err).To(BeNil())
				fenceAgent := v1alpha1.FenceAgentSpec{Name: updatedFenceAgentName, Type: fenceAgentType, Params: map[string]string{}}
				existingCR.Spec.FenceAgentsSpec = []v1alpha1.FenceAgentSpec{fenceAgent}
				Expect(k8sClient.Update(context.Background(), existingCR)).ToNot(HaveOccurred())

			case doNothing:
			}

		})

		Context("Sunny Flows", func() {

			AfterEach(func() {
				//Clean up after test
				cleanUp()
			})

			When("CR is not created", func() {
				BeforeEach(func() {
					userAction = doNothing
				})

				It("HA layer pod isn't created", func() {
					verifyHADeploymentIsMissing()
					verifyHAPodIsMissing()
					verifyCmdCommands(doNothing)
				})

			})

			When("CR is created", func() {

				BeforeEach(func() {
					userAction = createCR
				})

				It("HA layer pod is created", func() {
					verifyHADeploymentExist()
					verifyHAPodExist()
					verifyCmdCommands(createCR)

				})

			})

			When("CR is deleted", func() {
				BeforeEach(func() {
					userAction = deleteCR
					//Trigger pod creation so it can be deleted
					Expect(k8sClient.Create(context.Background(), underTest)).ToNot(HaveOccurred())
					verifyCRIsCreated()
					verifyHADeploymentExist()
					verifyHAPodExist()

				})

				It("HA layer pod is deleted", func() {
					verifyCrIsMissing()
					verifyHADeploymentIsMissing()
					verifyHAPodIsMissing()
					verifyCmdCommands(deleteCR)
				})

			})
			When("CR is updated", func() {

				BeforeEach(func() {
					//Trigger pod creation so it can be updated
					Expect(k8sClient.Create(context.Background(), underTest)).ToNot(HaveOccurred())
					verifyCRIsCreated()
					verifyHADeploymentExist()
					verifyHAPodExist()
					userAction = updateCR
				})

				It("HA layer pod is updated", func() {
					verifyHADeploymentExist()
					verifyHAPodExist()
					verifyCmdCommands(updateCR)

				})

			})
		})
		Context("Rainy Flows", func() {

		})

	})

})

func verifyCmdCommands(action userAction) {
	switch action {
	case doNothing:
		Eventually(func() int { return len(execCmdCommands) }, time.Second, time.Millisecond*10).Should(BeEquivalentTo(0))
	case createCR:
		Eventually(func() int { return len(execCmdCommands) }, time.Second*3, time.Millisecond*10).Should(BeEquivalentTo(1))
		expectedCommands := [][]string{
			{"pcs", "stonith", "create", originalFenceAgentName, fenceAgentType},
		}
		verifyExpectedCommands(expectedCommands)
	case deleteCR:
		Eventually(func() int { return len(execCmdCommands) }, time.Second, time.Millisecond*10).Should(BeEquivalentTo(3))
		expectedCommands := [][]string{
			{"pcs", "status", "xml"},
			{"pcs", "stonith", "create", originalFenceAgentName, fenceAgentType},
			{"pcs", "resource", "remove", originalFenceAgentName, "--force"},
		}
		verifyExpectedCommands(expectedCommands)
	case updateCR:
		Eventually(func() int { return len(execCmdCommands) }, time.Second*5, time.Millisecond*10).Should(BeEquivalentTo(5))
		expectedCommands := [][]string{
			{"pcs", "stonith", "create", originalFenceAgentName, fenceAgentType},
			{"pcs", "status", "xml"}, //appears twice
			{"pcs", "resource", "remove", originalFenceAgentName, "--force"},
			{"pcs", "stonith", "create", updatedFenceAgentName, fenceAgentType},
		}
		verifyExpectedCommands(expectedCommands)
	}

}

func verifyExpectedCommands(expectedCommands [][]string) {
	var actualCommands [][]string
	for len(execCmdCommands) > 0 {
		actualCommand := <-execCmdCommands
		actualCommands = append(actualCommands, actualCommand.command)
	}
	for _, expectedCommand := range expectedCommands {
		Expect(containsStringSlice(expectedCommand, actualCommands)).To(BeTrue())
	}
}

func containsStringSlice(expectedCommand []string, commands [][]string) bool {
	for _, actualCommand := range commands {
		if len(actualCommand) == len(expectedCommand) {
			isCommandMatch := true
			for i := 0; i < len(actualCommand); i++ {
				if actualCommand[i] != expectedCommand[i] {
					isCommandMatch = false
					break
				}
			}
			if isCommandMatch {
				return true
			}
		}
	}
	return false
}

func verifyHAPodIsMissing() {
	Eventually(
		func() bool {
			_, err := getHALayerPod()
			return errors.IsNotFound(err)
		}, time.Second*10, time.Millisecond*10).Should(BeTrue())
}

func verifyHADeploymentIsMissing() {
	Eventually(
		func() bool {
			_, err := spyReconciler.getHADeployment(defaultNamespace)
			return errors.IsNotFound(err)
		}, time.Second*10, time.Millisecond*10).Should(BeTrue())
}

func verifyCrIsMissing() {
	Eventually(
		func() bool {
			_, err := getHALayerCR()
			return errors.IsNotFound(err)
		}, time.Second*10, time.Millisecond*10).Should(BeTrue())
}

func verifyHADeploymentExist() bool {
	return Eventually(
		func() bool {
			_, err := spyReconciler.getHADeployment(defaultNamespace)
			return err == nil
		}, time.Second*10, time.Millisecond*10).Should(BeTrue())
}

func verifyHAPodExist() bool {
	return Eventually(
		func() bool {
			_, err := getHALayerPod()
			return err == nil
		}, time.Second*10, time.Millisecond*10).Should(BeTrue())
}

func verifyCRIsCreated() bool {
	return Eventually(
		func() bool {
			_, err := getHALayerCR()
			return err == nil
		}, time.Second*10, time.Millisecond*10).Should(BeTrue())
}

func cleanExecCommandsChannel() {
	execCmdCommands = make(chan execCmdCommand, 1000)
	Consistently(func() int { return len(execCmdCommands) }, time.Second, time.Millisecond*10).Should(BeEquivalentTo(0))
}

func cleanUp() {
	Eventually(deleteHALayerCR, time.Second*2, time.Millisecond*10).ShouldNot(HaveOccurred())
	verifyCrIsMissing()
	verifyHADeploymentIsMissing()
	verifyHAPodIsMissing()
	cleanExecCommandsChannel()
}
func deleteHALayerCR() error {
	hals, err := getHALayerCR()
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := k8sClient.Delete(context.Background(), hals); err != nil {
		return err
	}
	return nil
}

func createHALayerSetCR() *v1alpha1.HALayerSet {
	ha := &v1alpha1.HALayerSet{}
	ha.Name = haLayerCRName
	ha.Namespace = defaultNamespace
	fenceAgent := v1alpha1.FenceAgentSpec{Name: originalFenceAgentName, Type: fenceAgentType, Params: map[string]string{}}
	ha.Spec = v1alpha1.HALayerSetSpec{FenceAgentsSpec: []v1alpha1.FenceAgentSpec{fenceAgent}}
	ha.Spec.NodesSpec = v1alpha1.NodesSpec{FirstNodeName: node1Name, FirstNodeIP: "192.168.126.10", SecondNodeName: node2Name, SecondNodeIP: "192.168.126.11"}
	ha.Finalizers = []string{haSnoFinalizer}
	return ha
}

func beforeSuite() {
	Expect(k8sClient.Create(context.Background(), createPod("dummy"))).ToNot(HaveOccurred())
	Expect(os.Setenv(deploymentNamespaceEnvVar, defaultNamespace)).ToNot(HaveOccurred())

}

func afterSuite() {
	Expect(k8sClient.Delete(context.Background(), createPod("dummy"))).ToNot(HaveOccurred())
	Expect(os.Unsetenv(deploymentNamespaceEnvVar)).ToNot(HaveOccurred())
}

func createPod(name string) *v1.Pod {
	pod := &v1.Pod{}
	pod.SetNamespace(defaultNamespace)
	pod.SetName(name)
	c := v1.Container{Name: fmt.Sprintf("%s-container", name), Image: fmt.Sprintf("%s-image", name)}
	pod.Spec.Containers = []v1.Container{c}
	pod.Spec.NodeName = node1Name
	return pod
}

func getHALayerPod() (*v1.Pod, error) {
	//return spyReconciler.getHAPod(defaultNamespace)

	key := client.ObjectKey{Name: haLayerPodName, Namespace: defaultNamespace}
	pod := new(v1.Pod)
	if err := k8sClient.Get(context.Background(), key, pod); err == nil {
		return pod, nil
	} else {
		return nil, err
	}

}

func getHALayerCR() (*v1alpha1.HALayerSet, error) {
	key := client.ObjectKey{Name: haLayerCRName, Namespace: defaultNamespace}
	hals := new(v1alpha1.HALayerSet)
	if err := k8sClient.Get(context.Background(), key, hals); err == nil {
		return hals, nil
	} else {
		return nil, err
	}
}
