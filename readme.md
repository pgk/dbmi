# dbmi

Simple Postgres db schema migrations.

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

A new file is created in you migrations folder:

```sql
-- put your up-migration here.
/*DOWN*/
-- put your down-migration here.

```

Now fill in your schema change and the change that reverses it.

Migrate

```
dbmi migrate up
```

Down-migrate

```
dbmi migrate down 1
```
