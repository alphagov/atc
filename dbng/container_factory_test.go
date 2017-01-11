package dbng_test

import (
	"github.com/concourse/atc"
	"github.com/concourse/atc/dbng"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ContainerFactory", func() {
	Describe("FindContainersMarkedForDeletion", func() {
		It("does not find non-deleting containers", func() {
			deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
			Expect(err).NotTo(HaveOccurred())

			Expect(deletingContainers).To(BeEmpty())
		})

		It("does find deleting containers", func() {
			destroyingContainer, err := defaultCreatedContainer.Destroying()
			Expect(err).NotTo(HaveOccurred())

			deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
			Expect(err).NotTo(HaveOccurred())

			Expect(deletingContainers).To(HaveLen(1))

			destroyedContainer := deletingContainers[0]
			Expect(destroyedContainer.Handle()).To(Equal(destroyingContainer.Handle()))
			Expect(destroyedContainer.WorkerName()).To(Equal(destroyingContainer.WorkerName()))
		})
	})

	Describe("MarkBuildContainersForDeletion", func() {
		var (
			creatingContainer dbng.CreatingContainer
			build             *dbng.Build
		)

		BeforeEach(func() {
			build, err = defaultPipeline.CreateJobBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			creatingContainer, err = containerFactory.CreateBuildContainer(defaultWorker, build, atc.PlanID("some-job"), dbng.ContainerMetadata{Type: "task", Name: "some-task"})
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when the container is creating", func() {
			It("does not mark the container for deletion", func() {
				err = containerFactory.MarkBuildContainersForDeletion()
				Expect(err).NotTo(HaveOccurred())

				deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
				Expect(err).NotTo(HaveOccurred())

				Expect(deletingContainers).To(BeEmpty())
			})
		})

		Context("when the container is created", func() {
			var createdContainer dbng.CreatedContainer

			BeforeEach(func() {
				createdContainer, err = creatingContainer.Created()
			})

			Context("when the build is not finished", func() {
				It("does not mark the container for deletion", func() {
					err = containerFactory.MarkBuildContainersForDeletion()
					Expect(err).NotTo(HaveOccurred())

					deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
					Expect(err).NotTo(HaveOccurred())

					Expect(deletingContainers).To(BeEmpty())

				})
			})

			Context("when the build failed and there is a more recent build which has finished", func() {
				var (
					laterBuild             *dbng.Build
					laterCreatingContainer dbng.CreatingContainer
				)

				BeforeEach(func() {
					laterBuild, err = defaultPipeline.CreateJobBuild("some-job")
					Expect(err).NotTo(HaveOccurred())

					tx, err := dbConn.Begin()
					Expect(err).NotTo(HaveOccurred())

					err = laterBuild.Finish(tx, dbng.BuildStatusSucceeded)
					Expect(err).NotTo(HaveOccurred())

					err = build.Finish(tx, dbng.BuildStatusFailed)
					Expect(err).NotTo(HaveOccurred())

					Expect(tx.Commit()).To(Succeed())

					laterCreatingContainer, err = containerFactory.CreateBuildContainer(defaultWorker, build, atc.PlanID("some-job"), dbng.ContainerMetadata{Type: "task", Name: "some-task"})
					Expect(err).NotTo(HaveOccurred())

					_, err = laterCreatingContainer.Created()
					Expect(err).NotTo(HaveOccurred())
				})

				It("marks the older container for deletion", func() {
					err = containerFactory.MarkBuildContainersForDeletion()
					Expect(err).NotTo(HaveOccurred())

					deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
					Expect(err).NotTo(HaveOccurred())

					Expect(deletingContainers).ToNot(BeEmpty())
					Expect(deletingContainers[0].Handle()).NotTo(Equal(laterCreatingContainer.Handle()))
				})
			})

			Context("when there is a more recent build which is started and not finished", func() {
				var (
					laterBuild *dbng.Build

					laterCreatingContainer dbng.CreatingContainer
				)

				BeforeEach(func() {
					laterBuild, err = defaultPipeline.CreateJobBuild("some-job")
					Expect(err).NotTo(HaveOccurred())

					tx, err := dbConn.Begin()
					Expect(err).NotTo(HaveOccurred())

					err = laterBuild.SaveStatus(tx, dbng.BuildStatusStarted)
					Expect(err).NotTo(HaveOccurred())

					Expect(tx.Commit()).To(Succeed())

					laterCreatingContainer, err = containerFactory.CreateBuildContainer(defaultWorker, laterBuild, atc.PlanID("some-job"), dbng.ContainerMetadata{Type: "task", Name: "some-task"})
					Expect(err).NotTo(HaveOccurred())

					_, err = laterCreatingContainer.Created()
					Expect(err).NotTo(HaveOccurred())
				})

				Context("when the build is failing", func() {
					BeforeEach(func() {
						tx, err := dbConn.Begin()
						Expect(err).NotTo(HaveOccurred())

						err = build.Finish(tx, dbng.BuildStatusFailed)
						Expect(err).NotTo(HaveOccurred())

						Expect(tx.Commit()).To(Succeed())
					})

					It("does not mark the container for deletion", func() {
						err = containerFactory.MarkBuildContainersForDeletion()
						Expect(err).NotTo(HaveOccurred())

						deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
						Expect(err).NotTo(HaveOccurred())

						Expect(deletingContainers).To(BeEmpty())
					})
				})

				Context("when the build errors", func() {
					BeforeEach(func() {
						tx, err := dbConn.Begin()
						Expect(err).NotTo(HaveOccurred())

						err = build.Finish(tx, dbng.BuildStatusErrored)
						Expect(err).NotTo(HaveOccurred())

						Expect(tx.Commit()).To(Succeed())
					})

					It("does not mark the container for deletion", func() {
						err = containerFactory.MarkBuildContainersForDeletion()
						Expect(err).NotTo(HaveOccurred())

						deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
						Expect(err).NotTo(HaveOccurred())

						Expect(deletingContainers).To(BeEmpty())
					})
				})

				Context("when the build is aborted", func() {
					BeforeEach(func() {
						tx, err := dbConn.Begin()
						Expect(err).NotTo(HaveOccurred())

						err = build.Finish(tx, dbng.BuildStatusAborted)
						Expect(err).NotTo(HaveOccurred())

						Expect(tx.Commit()).To(Succeed())
					})

					It("does not mark the container for deletion", func() {
						err = containerFactory.MarkBuildContainersForDeletion()
						Expect(err).NotTo(HaveOccurred())

						deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
						Expect(err).NotTo(HaveOccurred())

						Expect(deletingContainers).To(BeEmpty())
					})
				})

				Context("when the build passed", func() {
					BeforeEach(func() {
						tx, err := dbConn.Begin()
						Expect(err).NotTo(HaveOccurred())

						err = build.Finish(tx, dbng.BuildStatusSucceeded)
						Expect(err).NotTo(HaveOccurred())

						Expect(tx.Commit()).To(Succeed())
					})

					It("marks the container for deletion", func() {
						err = containerFactory.MarkBuildContainersForDeletion()
						Expect(err).NotTo(HaveOccurred())

						deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
						Expect(err).NotTo(HaveOccurred())

						Expect(deletingContainers).To(HaveLen(1))
					})
				})
			})

			Context("when this is the most recent build", func() {
				Context("when the build is failing", func() {
					BeforeEach(func() {
						tx, err := dbConn.Begin()
						Expect(err).NotTo(HaveOccurred())

						err = build.Finish(tx, dbng.BuildStatusFailed)
						Expect(err).NotTo(HaveOccurred())

						Expect(tx.Commit()).To(Succeed())
					})

					It("does not mark the container for deletion", func() {
						err = containerFactory.MarkBuildContainersForDeletion()
						Expect(err).NotTo(HaveOccurred())

						deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
						Expect(err).NotTo(HaveOccurred())

						Expect(deletingContainers).To(BeEmpty())
					})
				})

				Context("when the build errors", func() {
					BeforeEach(func() {
						tx, err := dbConn.Begin()
						Expect(err).NotTo(HaveOccurred())

						err = build.Finish(tx, dbng.BuildStatusErrored)
						Expect(err).NotTo(HaveOccurred())

						Expect(tx.Commit()).To(Succeed())
					})

					It("does not mark the container for deletion", func() {
						err = containerFactory.MarkBuildContainersForDeletion()
						Expect(err).NotTo(HaveOccurred())

						deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
						Expect(err).NotTo(HaveOccurred())

						Expect(deletingContainers).To(BeEmpty())
					})
				})

				Context("when the build is aborted", func() {
					BeforeEach(func() {
						tx, err := dbConn.Begin()
						Expect(err).NotTo(HaveOccurred())

						err = build.Finish(tx, dbng.BuildStatusAborted)
						Expect(err).NotTo(HaveOccurred())

						Expect(tx.Commit()).To(Succeed())
					})

					It("does not mark the container for deletion", func() {
						err = containerFactory.MarkBuildContainersForDeletion()
						Expect(err).NotTo(HaveOccurred())

						deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
						Expect(err).NotTo(HaveOccurred())

						Expect(deletingContainers).To(BeEmpty())
					})
				})

				Context("when the build passed", func() {
					BeforeEach(func() {
						tx, err := dbConn.Begin()
						Expect(err).NotTo(HaveOccurred())

						err = build.Finish(tx, dbng.BuildStatusSucceeded)
						Expect(err).NotTo(HaveOccurred())

						Expect(tx.Commit()).To(Succeed())
					})

					It("marks the container for deletion", func() {
						_, foundCreatedContainer, err := containerFactory.FindBuildContainer(defaultWorker, build, atc.PlanID("some-job"), dbng.ContainerMetadata{Type: "task", Name: "some-task"})
						Expect(err).NotTo(HaveOccurred())
						Expect(foundCreatedContainer).NotTo(BeNil())

						err = containerFactory.MarkBuildContainersForDeletion()
						Expect(err).NotTo(HaveOccurred())

						deletingContainers, err := containerFactory.FindContainersMarkedForDeletion()
						Expect(err).NotTo(HaveOccurred())

						Expect(deletingContainers).To(HaveLen(1))
					})
				})
			})
		})
	})

	Describe("FindResourceCheckContainer", func() {
		var resourceConfig *dbng.UsedResourceConfig

		BeforeEach(func() {
			resourceConfig, err = resourceConfigFactory.FindOrCreateResourceConfigForResource(
				logger,
				defaultResource,
				"some-base-resource-type",
				atc.Source{"some": "source"},
				defaultPipeline.ID(),
				atc.ResourceTypes{},
			)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when there is a creating container", func() {
			BeforeEach(func() {
				_, err := containerFactory.CreateResourceCheckContainer(defaultWorker, resourceConfig)
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns it", func() {
				creatingContainer, createdContainer, err := containerFactory.FindResourceCheckContainer(defaultWorker, resourceConfig)
				Expect(err).NotTo(HaveOccurred())
				Expect(createdContainer).To(BeNil())
				Expect(creatingContainer).NotTo(BeNil())
			})
		})

		Context("when there is a created container", func() {
			BeforeEach(func() {
				creatingContainer, err := containerFactory.CreateResourceCheckContainer(defaultWorker, resourceConfig)
				Expect(err).NotTo(HaveOccurred())
				_, err = creatingContainer.Created()
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns it", func() {
				creatingContainer, createdContainer, err := containerFactory.FindResourceCheckContainer(defaultWorker, resourceConfig)
				Expect(err).NotTo(HaveOccurred())
				Expect(createdContainer).NotTo(BeNil())
				Expect(creatingContainer).To(BeNil())
			})
		})

		Context("when there is no container", func() {
			It("returns nil", func() {
				creatingContainer, createdContainer, err := containerFactory.FindResourceCheckContainer(defaultWorker, resourceConfig)
				Expect(err).NotTo(HaveOccurred())
				Expect(createdContainer).To(BeNil())
				Expect(creatingContainer).To(BeNil())
			})
		})
	})

	Describe("FindResourceGetContainer", func() {
		var containerMetadata dbng.ContainerMetadata

		BeforeEach(func() {
			containerMetadata = dbng.ContainerMetadata{
				Type: "task",
				Name: "some-task",
			}
		})

		Context("when there is a creating container", func() {
			BeforeEach(func() {
				_, err := containerFactory.CreateBuildContainer(defaultWorker, defaultBuild, "some-plan", containerMetadata)
				Expect(err).ToNot(HaveOccurred())
			})

			It("returns it", func() {
				creatingContainer, createdContainer, err := containerFactory.FindBuildContainer(defaultWorker, defaultBuild, "some-plan", containerMetadata)
				Expect(err).NotTo(HaveOccurred())
				Expect(createdContainer).To(BeNil())
				Expect(creatingContainer).NotTo(BeNil())
			})
		})

		Context("when there is a created container", func() {
			BeforeEach(func() {
				creatingContainer, err := containerFactory.CreateBuildContainer(defaultWorker, defaultBuild, "some-plan", containerMetadata)
				Expect(err).NotTo(HaveOccurred())
				_, err = creatingContainer.Created()
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns it", func() {
				creatingContainer, createdContainer, err := containerFactory.FindBuildContainer(defaultWorker, defaultBuild, "some-plan", containerMetadata)
				Expect(err).NotTo(HaveOccurred())
				Expect(createdContainer).NotTo(BeNil())
				Expect(creatingContainer).To(BeNil())
			})
		})

		Context("when there is no container", func() {
			It("returns nil", func() {
				creatingContainer, createdContainer, err := containerFactory.FindBuildContainer(defaultWorker, defaultBuild, "some-plan", containerMetadata)
				Expect(err).NotTo(HaveOccurred())
				Expect(createdContainer).To(BeNil())
				Expect(creatingContainer).To(BeNil())
			})
		})
	})
})
