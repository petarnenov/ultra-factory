# Build graph for the Railway images, so one checkout produces both.
# Run it from the repository root:
#   docker buildx bake -f deploy/railway/docker-bake.hcl hub

# The hub target extends the worker target through the `ao-worker` named build
# context, so the daemon, its agent CLIs, and tini inside the hub come from the
# commit being built. Nothing waits on a separately published worker tag.

# AO_VERSION and AO_COMMIT are read from the environment, as in the workflows.
variable "AO_VERSION" { default = "dev" }
variable "AO_COMMIT" { default = "unknown" }

# Comma-separated lists, empty when a build needs neither.
variable "PLATFORMS" { default = "" }
variable "TAGS" { default = "" }

target "worker" {
  dockerfile = "deploy/railway/worker/Dockerfile"
  args = {
    AO_VERSION = AO_VERSION
    AO_COMMIT  = AO_COMMIT
  }
}

target "hub" {
  dockerfile = "deploy/railway/hub/Dockerfile"
  contexts   = { "ao-worker" = "target:worker" }
  platforms  = compact(split(",", PLATFORMS))
  tags       = compact(split(",", TAGS))
}
