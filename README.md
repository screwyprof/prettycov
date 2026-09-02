# Prettycov
[![codecov](https://codecov.io/gh/screwyprof/prettycov/graph/badge.svg)](https://codecov.io/gh/screwyprof/prettycov) [![Go](https://github.com/screwyprof/prettycov/actions/workflows/go.yml/badge.svg)](https://github.com/screwyprof/prettycov/actions/workflows/go.yml)<!-- ALL-CONTRIBUTORS-BADGE:START - Do not remove or modify this section -->
[![All Contributors](https://img.shields.io/badge/all_contributors-1-orange.svg?style=flat-square)](#contributors-)
<!-- ALL-CONTRIBUTORS-BADGE:END --> 

Pretty Golang Coverage.

The other day I wanted to output a pretty overall coverage summary in my terminal.
I wanted to show a table or a tree with top-level packages and their corresponding coverage. 
I tried to search for some ready to use tools which would offer something similar but with not luck.
After that, I decided to build it on my own. So here it is :)

## Installation
```shell
go install github.com/screwyprof/prettycov/cmd/prettycov@latest
```

## How to use
### Getting built-in help
Run `prettycov` or `prettycov help` or `prettycov --help` to get build-in usage info.

### Run your tests with coverage
`prettycov` works by parsing coverage profile, so the first thing to do is to run tests with coverage:

`go test -cover -coverprofile=coverage.out` ./...

### Show coverage summary up to the given depth
You must specify the path to the coverage profile via `-profile` flag.

You may also specify `-depth` to set the maximum depth (starting from 0) of the resulting tree:

```shell
❯ prettycov -profile=coverage.out -depth=5
 github.com - 91.08
 └ screwyprof - 91.08
   └ skeleton - 91.08
     ├ internal - 85.73
     │ ├ delivery - 100.00
     │ │ └ rest - 100.00
     │ ├ app - 57.18
     │ │ ├ modzap - 16.70
     │ │ ├ modcfg - 75.00
     │ │ ├ modrel - 87.00
     │ │ └ fxlogger - 50.00
     │ └ adapter - 100.00
     │   └ postgres - 100.00
     ├ cert - 100.00
     │ └ usecase - 100.00
     │   ├ issuecert - 100.00
     │   └ viewcert - 100.00
     └ tests - 87.50
       └ integration - 87.50
         └ postgres - 87.50
```

### Show coverage summary with replaced paths
Sometimes the project may have a long project path (package path to be more precise) which clutters the output. 
In this case you may want to replace it with a shorter name:

```shell
❯ prettycov -profile=coverage.out -depth=5 -old github.com/screwyprof/skeleton -new unicorn
 unicorn - 91.08
 ├ tests - 87.50
 │ └ integration - 87.50
 │   └ postgres - 87.50
 │     └ app - 87.50
 ├ cert - 100.00
 │ └ usecase - 100.00
 │   ├ issuecert - 100.00
 │   └ viewcert - 100.00
 └ internal - 85.73
   ├ app - 57.18
   │ ├ fxlogger - 50.00
   │ ├ modcfg - 75.00
   │ ├ modrel - 87.00
   │ └ modzap - 16.70
   ├ delivery - 100.00
   │ └ rest - 100.00
   │   ├ apierr - 100.00
   │   ├ req - 100.00
   │   └ handler - 100.00
   └ adapter - 100.00
     └ postgres - 100.00
```

### Getting top-level coverage info
This is what I created this tool for. You may get a nice top-level package coverage:

```shell
❯ prettycov -profile=coverage.out -old github.com/screwyprof/skeleton -new unicorn -depth=1
 unicorn - 91.08
 ├ cert - 100.00
 ├ internal - 85.73
 └ tests - 87.50
```

## How it works
It parses the coverage profile to populate a prefix tree of paths and coverages.
Then it traverses the tree from the furthermost leaves to top merging the coverage info. 
Then it draws the tree up to the given depth.

### What's next?

#### Add tests and run `prettycov` in the CI to get the report for this project
This project was born spontaneously when I was on vacation. I didn't have much time, so just coded the idea with no tests.
Now that it proved to be useful it would be great to add tests. For now, I've got a red coverage badge to remind me about it.

## Contributors ✨
Thanks goes to these wonderful people ([emoji key](https://allcontributors.org/docs/en/emoji-key)):

<!-- ALL-CONTRIBUTORS-LIST:START - Do not remove or modify this section -->
<!-- prettier-ignore-start -->
<!-- markdownlint-disable -->
<table>
  <tbody>
    <tr>
      <td align="center"><a href="https://github.com/kannman"><img src="https://avatars.githubusercontent.com/u/40325995?v=4?s=100" width="100px;" alt=""/><br /><sub><b>antongr</b></sub></a><br /><a href="https://github.com/screwyprof/prettycov/commits?author=kannman" title="Code">💻</a></td>
    </tr>
  </tobdy>
</table>

<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->

<!-- ALL-CONTRIBUTORS-LIST:END -->

This project follows the [all-contributors](https://github.com/all-contributors/all-contributors) specification. Contributions of any kind welcome!