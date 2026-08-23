locals {
  name_prefix = "${var.name}-${var.environment}"
  public_host = var.route53_record_name != "" ? var.route53_record_name : var.domain_name

  common_tags = {
    Application = "smt"
    Component   = "zitadel"
    Environment = var.environment
    ManagedBy   = "opentofu"
  }
}

resource "aws_vpc" "this" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
}

resource "aws_subnet" "public" {
  count             = 2
  vpc_id            = aws_vpc.this.id
  availability_zone = var.availability_zones[count.index]
  cidr_block        = var.public_subnet_cidrs[count.index]

  map_public_ip_on_launch = false
}

resource "aws_subnet" "private" {
  count             = 2
  vpc_id            = aws_vpc.this.id
  availability_zone = var.availability_zones[count.index]
  cidr_block        = var.private_subnet_cidrs[count.index]
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
}

resource "aws_route_table_association" "public" {
  count          = 2
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

resource "aws_eip" "nat" {
  domain = "vpc"
}

resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public[0].id

  depends_on = [aws_internet_gateway.this]
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }
}

resource "aws_route_table_association" "private" {
  count          = 2
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private.id
}

resource "aws_security_group" "alb" {
  name_prefix = "${local.name_prefix}-alb-"
  description = "Public HTTPS and HTTP redirect for ZITADEL."
  vpc_id      = aws_vpc.this.id

  ingress {
    description = "HTTP redirect"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "Public HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "runtime" {
  name_prefix = "${local.name_prefix}-runtime-"
  description = "Private Compose host reachable only from the ALB and optional SSH sources."
  vpc_id      = aws_vpc.this.id

  ingress {
    description     = "HTTP/2 from the ALB"
    from_port       = var.compose_target_port
    to_port         = var.compose_target_port
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }

  dynamic "ingress" {
    for_each = var.ssh_ingress_cidrs
    content {
      description = "Optional operator SSH"
      from_port   = 22
      to_port     = 22
      protocol    = "tcp"
      cidr_blocks = [ingress.value]
    }
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "database" {
  name_prefix = "${local.name_prefix}-database-"
  description = "Private PostgreSQL access from the Compose host only."
  vpc_id      = aws_vpc.this.id

  ingress {
    description     = "PostgreSQL from Compose"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.runtime.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_lb" "this" {
  name                       = substr(replace(local.name_prefix, "_", "-"), 0, 32)
  internal                   = false
  load_balancer_type         = "application"
  security_groups            = [aws_security_group.alb.id]
  subnets                    = aws_subnet.public[*].id
  drop_invalid_header_fields = true
}

resource "aws_lb_target_group" "runtime" {
  name             = substr(replace("${local.name_prefix}-runtime", "_", "-"), 0, 32)
  port             = var.compose_target_port
  protocol         = "HTTP"
  protocol_version = "HTTP2"
  target_type      = "instance"
  vpc_id           = aws_vpc.this.id

  health_check {
    enabled             = true
    path                = "/debug/ready"
    protocol            = "HTTP"
    matcher             = "200"
    interval            = 30
    timeout             = 10
    healthy_threshold   = 3
    unhealthy_threshold = 3
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"

    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.this.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = var.certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.runtime.arn
  }
}

resource "aws_iam_role" "runtime" {
  name = substr(replace("${local.name_prefix}-runtime", "_", "-"), 0, 64)

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = {
        Service = "ec2.amazonaws.com"
      }
      Action = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.runtime.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role_policy_attachment" "cloudwatch" {
  role       = aws_iam_role.runtime.name
  policy_arn = "arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"
}

resource "aws_iam_instance_profile" "runtime" {
  name = substr(replace("${local.name_prefix}-runtime", "_", "-"), 0, 64)
  role = aws_iam_role.runtime.name
}

resource "aws_cloudwatch_log_group" "runtime" {
  name              = "/smt/${var.environment}/zitadel"
  retention_in_days = var.cloudwatch_retention_days
  kms_key_id        = aws_kms_key.runtime.arn
}

resource "aws_kms_key" "runtime" {
  description             = "KMS key for SMT ZITADEL RDS and runtime logs."
  deletion_window_in_days = 30
  enable_key_rotation     = true
}

resource "aws_kms_alias" "runtime" {
  name          = "alias/${local.name_prefix}"
  target_key_id = aws_kms_key.runtime.key_id
}

resource "aws_launch_template" "runtime" {
  name_prefix   = "${local.name_prefix}-"
  image_id      = var.ami_id
  instance_type = var.instance_type
  key_name      = var.key_name

  iam_instance_profile {
    name = aws_iam_instance_profile.runtime.name
  }

  network_interfaces {
    associate_public_ip_address = false
    security_groups             = [aws_security_group.runtime.id]
  }

  block_device_mappings {
    device_name = "/dev/xvda"

    ebs {
      encrypted   = true
      kms_key_id  = aws_kms_key.runtime.arn
      volume_size = var.root_volume_size_gib
      volume_type = "gp3"
    }
  }

  user_data = base64encode(<<-EOT
    #!/bin/bash
    set -euo pipefail
    install -d -m 0750 /opt/smt/zitadel
    printf '%s\n' 'Upload the generated Compose contract and production secret references through SSM.' > /opt/smt/zitadel/README
    printf 'ZITADEL_IMAGE=%s\nZITADEL_LOGIN_IMAGE=%s\nPROXY_IMAGE=%s\n' '${var.zitadel_image}' '${var.zitadel_login_image}' '${var.proxy_image}' > /opt/smt/zitadel/images.env
  EOT
  )
}

resource "aws_autoscaling_group" "runtime" {
  name                = local.name_prefix
  min_size            = 1
  max_size            = 1
  desired_capacity    = 1
  vpc_zone_identifier = aws_subnet.private[*].id
  health_check_type   = "EC2"
  target_group_arns   = [aws_lb_target_group.runtime.arn]

  launch_template {
    id      = aws_launch_template.runtime.id
    version = aws_launch_template.runtime.latest_version
  }

  tag {
    key                 = "Name"
    value               = local.name_prefix
    propagate_at_launch = true
  }
}

resource "aws_db_subnet_group" "runtime" {
  name       = local.name_prefix
  subnet_ids = aws_subnet.private[*].id
}

resource "aws_db_instance" "runtime" {
  identifier                    = local.name_prefix
  engine                        = "postgres"
  engine_version                = var.rds_engine_version
  instance_class                = var.rds_instance_class
  allocated_storage             = var.rds_storage_gib
  storage_type                  = "gp3"
  storage_encrypted             = true
  kms_key_id                    = aws_kms_key.runtime.arn
  multi_az                      = true
  publicly_accessible           = false
  db_name                       = "smt"
  username                      = "smt_admin"
  manage_master_user_password   = true
  master_user_secret_kms_key_id = aws_kms_key.runtime.arn
  port                          = 5432
  db_subnet_group_name          = aws_db_subnet_group.runtime.name
  vpc_security_group_ids        = [aws_security_group.database.id]
  backup_retention_period       = var.rds_backup_retention_days
  backup_window                 = "03:00-04:00"
  maintenance_window            = "sun:04:00-sun:05:00"
  deletion_protection           = var.rds_deletion_protection
  skip_final_snapshot           = var.rds_skip_final_snapshot
  copy_tags_to_snapshot         = true
  apply_immediately             = false
}

resource "aws_secretsmanager_secret" "zitadel_runtime" {
  name                    = "${local.name_prefix}/zitadel-runtime"
  description             = "Secret metadata for externally provisioned ZITADEL DSN and master key."
  kms_key_id              = aws_kms_key.runtime.arn
  recovery_window_in_days = 30
}

resource "aws_cloudwatch_metric_alarm" "runtime_cpu" {
  alarm_name          = "${local.name_prefix}-runtime-cpu"
  alarm_description   = "The single Compose host is CPU constrained."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "CPUUtilization"
  namespace           = "AWS/EC2"
  period              = 300
  statistic           = "Average"
  threshold           = 80
  treat_missing_data  = "notBreaching"

  dimensions = {
    AutoScalingGroupName = aws_autoscaling_group.runtime.name
  }
}

resource "aws_cloudwatch_metric_alarm" "unhealthy_targets" {
  alarm_name          = "${local.name_prefix}-unhealthy-targets"
  alarm_description   = "The ZITADEL Compose target is not passing /debug/ready."
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "UnHealthyHostCount"
  namespace           = "AWS/ApplicationELB"
  period              = 60
  statistic           = "Maximum"
  threshold           = 0
  treat_missing_data  = "breaching"

  dimensions = {
    LoadBalancer = aws_lb.this.arn_suffix
    TargetGroup  = aws_lb_target_group.runtime.arn_suffix
  }
}

resource "aws_route53_record" "runtime" {
  count   = var.route53_zone_id != null && local.public_host != "" ? 1 : 0
  zone_id = var.route53_zone_id
  name    = local.public_host
  type    = "A"

  alias {
    name                   = aws_lb.this.dns_name
    zone_id                = aws_lb.this.zone_id
    evaluate_target_health = true
  }
}
