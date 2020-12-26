# dbmi

Simple Postgres schema migrations.

## Examples

Create a config file

```
dbmi exampleconfig > dbmi.conf.json
```

And edit it to fit your setup.

Initialize schema migrations
```
dbmi init
```

Create a new migration

```
dbmi new 'create items table'
```

Migrate

```
dbmi migrate up
```

Down-migrate

```
dbmi migrate down 1
```
