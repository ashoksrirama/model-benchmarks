data "aws_availability_zones" "available" {
  filter {
    name   = "opt-in-status"
    values = ["opt-in-not-required"]
  }
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 3)
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 6.0"

  name = var.name
  cidr = var.cidr

  azs             = local.azs
  private_subnets = [for k, v in local.azs : cidrsubnet(var.cidr, 4, k)]
  public_subnets  = [for k, v in local.azs : cidrsubnet(var.cidr, 8, k + 48)]
  intra_subnets   = [for k, v in local.azs : cidrsubnet(var.cidr, 8, k + 52)]

  enable_nat_gateway = true
  single_nat_gateway = var.single_nat_gateway

  # The default route table has no functional use (the private/public/intra
  # RTs carry the real routes), and aws_default_route_table has a known
  # import bug in the AWS provider for RTs with only a local route.
  manage_default_route_table = false

  public_subnet_tags = {
    "kubernetes.io/role/elb" = 1
  }

  private_subnet_tags = {
    "kubernetes.io/role/internal-elb" = 1
    "karpenter.sh/discovery"          = var.cluster_name
  }

  tags = var.tags
}

# S3 Gateway VPC endpoint. Model weights (Run:ai streamer) and the ECR
# pull-through cache's S3-backed image layers are pulled from S3 on every
# benchmark run; a fresh GPU node per run means these pulls happen constantly.
# Without this endpoint that traffic egresses through the NAT gateway — a shared
# bandwidth ceiling + per-GB data-processing charge, and a bottleneck when
# several nodes pull at once (AWS's fast-model-loading guidance flags this).
# A Gateway endpoint keeps S3 traffic on the AWS network (no NAT, no per-GB fee)
# by injecting the S3 prefix-list route into the private route tables. Gateway
# endpoints are free. Greenfield only (this module isn't created in brownfield).
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = module.vpc.vpc_id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = module.vpc.private_route_table_ids
  tags              = merge(var.tags, { Name = "${var.name}-s3" })
}
