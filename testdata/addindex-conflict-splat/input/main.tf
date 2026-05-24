resource "aws_instance" "foo" {
  count = 2
  ami   = "ami-123"
}

# Splat — parsed as SplatExpr around a bare-looking ScopeTraversalExpr.
output "all_ids" {
  value = aws_instance.foo[*].id
}
