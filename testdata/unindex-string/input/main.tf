resource "zoo_thing" "baz" {
  for_each = toset(["hoge", "fuga"])
  name     = each.key
}

output "hoge_id" {
  value = zoo_thing.baz["hoge"].id
}

output "fuga_id" {
  value = zoo_thing.baz["fuga"].id
}
