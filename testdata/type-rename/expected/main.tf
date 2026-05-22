resource "aws_db_instance" "bar" {
  ami = "ami-123"
}

resource "aws_eip" "addr" {
  instance = aws_db_instance.bar.id
}
