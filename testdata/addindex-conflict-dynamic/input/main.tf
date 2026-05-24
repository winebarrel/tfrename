resource "aws_instance" "foo" {
  count = var.replicas
  ami   = "ami-123"
}

variable "replicas" { default = 2 }

# Dynamic index — parsed as IndexExpr around a bare-looking ScopeTraversalExpr.
# Without the IndexExpr check, addindex would silently rewrite the inner
# `aws_instance.foo` and produce `aws_instance.foo[0][var.replicas - 1]`.
output "last_id" {
  value = aws_instance.foo[var.replicas - 1].id
}
