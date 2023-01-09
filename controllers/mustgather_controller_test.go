/*
Copyright 2023.

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

package controllers

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	redhatcopv1alpha1 "github.com/redhat-cop/must-gather-operator/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sort"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/client-go/kubernetes/scheme"
)

type decoder func(data []byte, defaults *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error)

var _ = Describe("MustGather controller", func() {

	Context("MustGather controller test", func() {

		const MustGatherName = "test-mustgather"

		ctx := context.Background()

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      MustGatherName,
				Namespace: MustGatherName,
			},
		}
		typeNamespaceName := types.NamespacedName{Name: MustGatherName, Namespace: MustGatherName}

		var decoderFunc, err = getDecoder()
		Expect(err).ToNot(HaveOccurred())

		It("should successfully reconcile a custom resource for MustGather", func() {
			By("Creating the custom resource for the Kind MustGather")
			mustgather := &redhatcopv1alpha1.MustGather{}
			err := k8sClient.Get(ctx, typeNamespaceName, mustgather)
			if err != nil && errors.IsNotFound(err) {
				// Let's mock our custom resource at the same way that we would
				// apply on the cluster the manifest under config/samples
				mustgather := &redhatcopv1alpha1.MustGather{
					ObjectMeta: metav1.ObjectMeta{
						Name:      MustGatherName,
						Namespace: namespace.Name,
					},
					Spec: redhatcopv1alpha1.MustGatherSpec{},
				}

				err = k8sClient.Create(ctx, mustgather)
				Expect(err).To(Not(HaveOccurred()))
			}

			By("Checking if the custom resource was successfully created")
			Eventually(func() error {
				found := &redhatcopv1alpha1.MustGather{}
				return k8sClient.Get(ctx, typeNamespaceName, found)
			}, time.Minute, time.Second).Should(Succeed())

			By("Reconciling the custom resource created")
			mustgatherReconciler := &MustGatherReconciler{
				ReconcilerBase: util.NewReconcilerBase(k8sClient, k8sClient.Scheme(), cfg, nil, k8sClient),
				Log:            ctrl.Log.WithName("controllers").WithName("MustGather"),
			}
			mustgatherReconciler.init()

			result, err := mustgatherReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespaceName,
			})
			Expect(err).To(Not(HaveOccurred()))
			Expect(result.Requeue).To(BeFalse())

		})

		DescribeTable("should correctly render a job template - ...", func(testCaseName string) {
			By("Creating the custom resource for the Kind MustGather")

			mustgatherReconciler := &MustGatherReconciler{
				ReconcilerBase: util.NewReconcilerBase(k8sClient, k8sClient.Scheme(), cfg, nil, k8sClient),
				Log:            ctrl.Log.WithName("controllers").WithName("MustGather"),
			}
			mustgatherReconciler.init()

			os.Setenv(templateFileNameEnv, "../config/templates/job.template.yaml")

			err := mustgatherReconciler.initializeTemplate()
			Expect(err).To(Not(HaveOccurred()))

			mg, err := readMustGather(decoderFunc, testCaseName)
			Expect(err).To(Not(HaveOccurred()))

			initialized := mustgatherReconciler.IsInitialized(mg)
			if !initialized {
				initialized = mustgatherReconciler.IsInitialized(mg)
			}
			Expect(initialized).To(BeTrue())

			unstructured, err := mustgatherReconciler.getJobFromInstance(mg)
			Expect(err).To(Not(HaveOccurred()))

			var job batchv1.Job
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.UnstructuredContent(), &job)
			Expect(err).To(Not(HaveOccurred()))

			fileJob, err := readJob(decoderFunc, testCaseName)
			Expect(err).To(Not(HaveOccurred()))

			sortContainersByImageName(job.Spec.Template.Spec.Containers)
			sortContainersByImageName(job.Spec.Template.Spec.InitContainers)
			sortContainersByImageName(job.Spec.Template.Spec.Containers)
			sortContainersByImageName(job.Spec.Template.Spec.InitContainers)

			d := cmp.Diff(*fileJob, job)
			Expect(d).To(BeEmpty())
			Expect(job).To(BeEquivalentTo(*fileJob))

		},
			Entry(
				"example MustGather CR",
				"example",
			),
			Entry(
				"example MustGather CR with parameters",
				"exampleparam",
			),
			Entry(
				"full MustGather CR",
				"full",
			),

			Entry(
				"proxy MustGather CR",
				"proxy",
			),
			Entry(
				"exaustive MustGather CR",
				"exaustive",
			),
		)
	})
})

func getDecoder() (decoder, error) {
	sch := runtime.NewScheme()
	err := scheme.AddToScheme(sch)
	if err != nil {
		return nil, err
	}
	err = redhatcopv1alpha1.AddToScheme(sch)
	if err != nil {
		return nil, err
	}
	err = batchv1.AddToScheme(sch)
	if err != nil {
		return nil, err
	}
	return serializer.NewCodecFactory(sch).UniversalDeserializer().Decode, nil
}

func readMustGather(decoderFunc decoder, testCaseName string) (*redhatcopv1alpha1.MustGather, error) {
	stream, err := os.ReadFile("../test/mgs/" + testCaseName + ".yaml")
	obj, gKV, err := decoderFunc(stream, nil, nil)
	if err != nil {
		return nil, err
	}
	if gKV.Kind != "MustGather" {
		return nil, errors.NewInvalid(gKV.GroupKind(), "unexpected kind", nil)
	}
	mg := obj.(*redhatcopv1alpha1.MustGather)
	sort.Strings(mg.Spec.MustGatherImages)
	sort.SliceStable(mg.Spec.MustGatherImagesWithParameters, func(i, j int) bool {
		return mg.Spec.MustGatherImagesWithParameters[i].Image < mg.Spec.MustGatherImagesWithParameters[j].Image
	})
	return mg, nil
}

func readJob(decoderFunc decoder, testCaseName string) (*batchv1.Job, error) {
	stream, err := os.ReadFile("../test/jobs/" + testCaseName + ".yaml")
	if err != nil {
		return nil, err
	}
	obj, gKV, err := decoderFunc(stream, nil, nil)
	if err != nil {
		return nil, err
	}
	if gKV.Kind != "Job" {
		return nil, errors.NewInvalid(gKV.GroupKind(), "unexpected kind", nil)
	}
	job := obj.(*batchv1.Job)
	sortContainersByImageName(job.Spec.Template.Spec.Containers)
	sortContainersByImageName(job.Spec.Template.Spec.InitContainers)
	return job, nil
}

func sortContainersByImageName(containers []corev1.Container) {
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Image < containers[j].Image
	})
}
