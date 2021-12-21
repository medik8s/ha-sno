/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("HALayerSet Validation", func() {

	Describe("updating HALayerSet", func() {

		Context("removing deployment", func() {

			It("should succeed", func() {
				haOld, haNew := createHALayerSetCR(), createHALayerSetCR()
				haNew.Spec.Deployments = haOld.Spec.Deployments[1:]
				Expect(haNew.ValidateUpdate(haOld)).NotTo(HaveOccurred())
			})

		})

		Context("changing node name", func() {

			It("should fail", func() { //First Node Name
				haOld, haNew := createHALayerSetCR(), createHALayerSetCR()
				haNew.Spec.NodesSpec.FirstNodeName = "ClusterX"
				err := haNew.ValidateUpdate(haOld)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(nodeNameChangeErrorMsg))
			})

			It("should fail", func() { //Second Node Name
				haOld, haNew := createHALayerSetCR(), createHALayerSetCR()
				haNew.Spec.NodesSpec.SecondNodeName = "ClusterX"
				err := haNew.ValidateUpdate(haOld)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(nodeNameChangeErrorMsg))
			})

		})

		Context("changing node IP", func() {

			It("should fail", func() { //First Node IP
				haOld, haNew := createHALayerSetCR(), createHALayerSetCR()
				haNew.Spec.NodesSpec.FirstNodeIP = "192.168.126.20"
				err := haNew.ValidateUpdate(haOld)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(nodeIpChangeErrorMsg))
			})

			It("should fail", func() { //Second Node Name
				haOld, haNew := createHALayerSetCR(), createHALayerSetCR()
				haNew.Spec.NodesSpec.SecondNodeIP = "192.168.126.20"
				err := haNew.ValidateUpdate(haOld)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(nodeIpChangeErrorMsg))
			})

		})
	})
})

func createHALayerSetCR() *HALayerSet {
	ha := &HALayerSet{}
	ha.Name = "test"
	ha.Namespace = "default"
	fenceAgent := FenceAgentSpec{Name: "mock-fence-org", Type: "fence_mock", Params: map[string]string{}}
	ha.Spec = HALayerSetSpec{FenceAgentsSpec: []FenceAgentSpec{fenceAgent}}
	ha.Spec.NodesSpec = NodesSpec{FirstNodeName: "cluster1", FirstNodeIP: "192.168.126.10", SecondNodeName: "cluster2", SecondNodeIP: "192.168.126.11"}
	ha.Spec.Deployments = []string{"test-dep1", "test-dep2"}
	return ha
}
