### Install Go Mangle Interpreter

Source: <https://github.com/google/mangle/blob/main/readthedocs/installing.md>

Installs the Go implementation of the Mangle interpreter to a specified directory. Requires Go to be installed. The command uses 'go install' with a specific package path and sets the GOBIN environment variable.

```shell
GOBIN=~/bin go install github.com/google/mangle/interpreter/mg@latest
```

--------------------------------

### Start Mangle Interpreter

Source: <https://github.com/google/mangle/blob/main/docs/using_the_interpreter.md>

Starts the Mangle interactive interpreter from the command line.

```bash
go run interpreter/main/main.go
```

--------------------------------

### Mangle Name Syntax and Examples

Source: <https://github.com/google/mangle/blob/main/readthedocs/basictypes.md>

Demonstrates the syntax for Mangle names, which are used to refer to entities and objects. Names consist of parts starting with '/', followed by letters, digits, and specific punctuation. Examples show valid name formats.

```Mangle
/a
/test12
/antigone

/crates.io/fnv
/home.cern/news/news/computing/30-years-free-and-open-web
```

--------------------------------

### Python: Set up virtual environment for Sphinx

Source: <https://github.com/google/mangle/blob/main/readthedocs/README.md>

This snippet demonstrates how to create and activate a Python virtual environment for managing Sphinx and its dependencies. It installs Sphinx and sets an environment variable pointing to the Read the Docs directory, followed by installing project-specific requirements.

```bash
> python -m venv manglereadthedocs
> . manglereadthedocs/bin/activate
(manglereadthedocs) > pip install -U sphinx
(manglereadthedocs) > READTHEDOCS=<path to readthedocs dir>
(manglereadthedocs) > pip install -r ${READTHEDOCS}/requirements.txt
```

--------------------------------

### Start Mangle Interpreter with Custom Root Directory

Source: <https://github.com/google/mangle/blob/main/docs/using_the_interpreter.md>

Starts the Mangle interactive interpreter and specifies a custom root directory for loading files.

```bash
go run interpreter/main/main.go --root=$PWD/examples
```

--------------------------------

### Build Go Mangle from Source

Source: <https://github.com/google/mangle/blob/main/readthedocs/installing.md>

Builds the Go implementation of Mangle from its source code repository. This involves cloning the repository, navigating into the directory, fetching dependencies, building the project, and running tests. Requires Git and Go.

```shell
git clone https://github.com/google/mangle
cd mangle
go get -t ./...
go build ./...
go test ./...
```

--------------------------------

### Rectified Datalog Rule Example

Source: <https://github.com/google/mangle/blob/main/docs/spec_explain_relational_algebra.md>

Demonstrates how a Datalog rule with repeated variables is rewritten by introducing a fresh variable and an equation for rectification.

```datalog
knows(X, X) :- person(X).

```

```datalog
knows(X, Y) :- person(X), Y = X.

```

--------------------------------

### Build Rust Mangle from Source

Source: <https://github.com/google/mangle/blob/main/readthedocs/installing.md>

Builds the Rust implementation of Mangle from its source code repository. This involves cloning the repository, changing the directory to the Rust subdirectory, and then using Cargo to build and test the project. Requires Git and Rust (Cargo).

```shell
git clone https://github.com/google/mangle
cd mangle/rust
cargo build
cargo test
```

--------------------------------

### Python: Run local HTTP server for documentation preview

Source: <https://github.com/google/mangle/blob/main/readthedocs/README.md>

This command starts a local HTTP server to preview the generated documentation. It uses Python's built-in http.server module, allowing developers to view the documentation locally before deploying it. The server runs on port 8080.

```bash
(manglereadthedocs) > python3 -m http.server 8080
```

--------------------------------

### Mangle Singleton Type Example

Source: <https://github.com/google/mangle/blob/main/readthedocs/typeexpressions.md>

Illustrates the singleton type constructor in Mangle. For every name in a program, a unique singleton type is generated.

```Mangle
fn:Singleton(/foo)
```

--------------------------------

### Example Fact with Structured Data in Mangle

Source: <https://github.com/google/mangle/blob/main/docs/structured_data.md>

A combined example showing a Mangle fact that includes a list containing three struct constants, each representing a point with /x and /y coordinates.

```Mangle
triangle_2d([
               { /x: 1,  /y:  2 },
               { /x: 5, /y: 10 },
               { /x: 12,  /y:  5 }
             ])
```

--------------------------------

### Mangle Type Variables Example

Source: <https://github.com/google/mangle/blob/main/readthedocs/typeexpressions.md>

Demonstrates the use of type variables in Mangle type expressions. Type variables are denoted by starting with a capital letter.

```Mangle
X
fn:Pair(Y, /string)
```

--------------------------------

### Mangle Floating-Point Number Examples

Source: <https://github.com/google/mangle/blob/main/readthedocs/basictypes.md>

Shows examples of Mangle floating-point number literals, which represent 64-bit floating-point values.

```Mangle
3.141592
-10.5
```

--------------------------------

### Mangle Integer Number Examples

Source: <https://github.com/google/mangle/blob/main/readthedocs/basictypes.md>

Provides examples of Mangle integer number literals. These are 64-bit signed integers within a specific range.

```Mangle
0
1
128
-10000
```

--------------------------------

### Datalog Facts for PhD Supervision

Source: <https://github.com/google/mangle/blob/main/docs/spec_datamodel.md>

Provides examples of Datalog facts representing the 'phd_supervised_by' relationship, showing multiple entries for different individuals and their supervisors.

```datalog
phd_supervised_by(/jacques_herbrand, /topic/math_logic, /ernest_vessiot)
phd_supervised_by(/julia_robinson,   /topic/math_logic, /alfred_tarski)
phd_supervised_by(/raymond_smullyan, /topic/math_logic, /alonzo_church)
```

--------------------------------

### Datalog Facts Examples

Source: <https://github.com/google/mangle/blob/main/docs/spec_datamodel.md>

Illustrates the definition of facts in Datalog, which are predicate symbols applied to constant symbols. These facts represent relationships between objects.

```datalog
p(/asdf)
loves(/hilbert, /topic/mathematics)
phd_supervised_by(/jacques_herbrand, /topic/math_logic, /ernest_vessiot)
```

--------------------------------

### Defining Facts in Mangle

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

This shows how to represent data as facts in Mangle. Each fact is a predicate with arguments, ending with a period. This is useful for initial data loading or representing simple relationships.

```Mangle
volunteer(1, "Aisha Salehi", /teaching).
volunteer(1, "Aisha Salehi", /workshop_facilitation).
volunteer(2, "Xin Watson", /workshop_facilitation).
volunteer(3, "Alyssa P. Hacker", /software_development).
volunteer(3, "Alyssa P. Hacker", /workshop_facilitation).
```

--------------------------------

### SQL Translation of Datalog Teacher Rule

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

Provides the SQL equivalent for the Datalog 'teacher' rule. This SQL query selects the ID and Name from the 'volunteer' table where the 'Role' column matches '/teaching', demonstrating how Datalog rules can be mapped to relational database queries.

```sql
CREATE TABLE teacher AS
SELECT ID, Name
FROM volunteer
WHERE Role = '/teaching';
```

--------------------------------

### Combining Rules with Join in Mangle

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

Illustrates how to use multiple atoms in a rule's body, separated by a comma, to perform a join-like operation. This rule finds volunteers who possess both specified skills.

```Mangle
teacher_and_coder(ID, Name) <-
    volunteer(ID, Name, /teaching),
    volunteer(ID, Name, /software_development).
```

--------------------------------

### Example of Vocabulary Violation (Mangle)

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_db.md>

Demonstrates how Mangle enforces vocabulary constraints. Providing an invalid constant (like a day for a skill argument) will result in an error, highlighting the importance of declared bounds.

```mangle
volunteer_interest(/v/1, /monday). # This does not look right.

```

--------------------------------

### Constructing Maps in Mangle

Source: <https://github.com/google/mangle/blob/main/readthedocs/constructedtypes.md>

Provides examples of creating map data types in Mangle. Maps associate keys with values, represented as key:value pairs within square brackets. Trailing commas are permitted.

```Mangle
[/a: /foo, /b: /bar]
[0: "zero", 1: "one",]
[/a: 1,]
```

--------------------------------

### Datalog Facts for Volunteers

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

Defines facts representing volunteer data, where each fact corresponds to a row in a table. These are the simplest form of Datalog rules, stating what is true without any premises. The predicate name 'volunteer' corresponds to the table name, and arguments are the column values.

```datalog
volunteer(1, "Aisha Salehi", /teaching).
volunteer(2, "Xin Watson", /workshop_facilitation).
volunteer(3, "Alyssa P. Hacker", /software_development).
```

--------------------------------

### SQL Translation of Combined Rules (JOIN)

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

The SQL equivalent of a Mangle rule with multiple atoms in the body, demonstrating a self-join on the 'volunteer' table. It finds individuals who have both '/teaching' and '/software_development' roles.

```SQL
CREATE TABLE teacher_and_coder AS
SELECT DISTINCT v1.ID, v1.Name
FROM volunteer AS v1
JOIN volunteer AS v2 ON v1.ID = v2.ID AND v1.Name = v2.Name
WHERE
    v1.Role = '/teaching'
    AND v2.Role = '/software_development';
```

--------------------------------

### Mangle Union Types Examples

Source: <https://github.com/google/mangle/blob/main/readthedocs/typeexpressions.md>

Shows various forms of union types in Mangle, which are constructed by combining multiple type expressions. Includes the empty union type.

```Mangle
fn:Union()
fn:Union(/name, /string)
fn:Union(fn:Singleton(/foo), fn:Singleton(/bar))
```

--------------------------------

### Defining Facts for Recursive Rules in Mangle

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

Simple facts representing a 'knows' relationship between individuals. These facts serve as the base cases for recursive rules that explore connections.

```Mangle
knows("Aisha", "Xin").
knows("Xin", "Alyssa").
knows("Alyssa", "Selin").
```

--------------------------------

### Combining Rules with Union in Mangle

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

Demonstrates combining multiple rules using implicit union to create a new relation. Each rule defines a subset of the desired facts based on specific criteria. This is analogous to a UNION operation in SQL.

```Mangle
coding_workshop_candidate(ID, Name, /teaching) <-
    volunteer(ID, Name, /teaching).
coding_workshop_candidate(ID, Name, /software_development) <-
    volunteer(ID, Name, /software_development).
```

--------------------------------

### Mangle List Accessor Function (Get)

Source: <https://github.com/google/mangle/blob/main/docs/spec_builtin_operations.md>

Accesses the n-th member of a list using a zero-based index. This allows retrieval of list elements by their position.

```Mangle
fn:list:get(ListValue, Index)
```

--------------------------------

### SQL Translation of Recursive Rules

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

The SQL equivalent of the recursive 'reachable' rules in Mangle, utilizing a Common Table Expression (CTE) with the RECURSIVE keyword. This demonstrates how to find all reachable individuals in a network.

```SQL
CREATE TABLE reachable AS
WITH RECURSIVE reachable_cte (StartPerson, EndPerson) AS (
    -- Base Case
    SELECT Person1, Person2
    FROM knows

    UNION ALL

    -- Recursive Step
    SELECT
        r.StartPerson,
        k.Person2
    FROM
        reachable_cte AS r
    JOIN
        knows AS k ON r.EndPerson = k.Person1
)
SELECT DISTINCT StartPerson, EndPerson
FROM reachable_cte;
```

--------------------------------

### Defining Recursive Rules in Mangle

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

Two rules defining a 'reachable' relation. The first rule is the base case, and the second rule is the recursive step, allowing the traversal of connections. This is analogous to finding paths in a graph.

```Mangle
reachable(X, Y) <-
    knows(X, Y).
reachable(X, Z) <-
    knows(X, Y),
    reachable(Y, Z).
```

--------------------------------

### Datalog Rule to Identify Teachers

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

Defines a Datalog rule to infer new facts (teachers) from existing ones. The 'teacher' predicate is defined based on the 'volunteer' predicate, selecting volunteers with the '/teaching' skill. This demonstrates rule-based deduction in Datalog.

```datalog
teacher(ID, Name) ⟸ volunteer(ID, Name, /teaching).
```

--------------------------------

### SQL Translation of Combined Rules (UNION)

Source: <https://github.com/google/mangle/blob/main/readthedocs/datalog.md>

The SQL equivalent of combining Mangle rules using the UNION operator. It selects distinct rows from the 'volunteer' table based on different 'Role' conditions, mirroring the behavior of the Mangle rules.

```SQL
CREATE TABLE coding_workshop_candidate AS
SELECT ID, Name, Role
FROM volunteer
WHERE Role = '/teaching'
UNION
SELECT ID, Name, Role
FROM volunteer
WHERE Role = '/software_development';
```

--------------------------------

### Define Family Facts and Sibling Rule in Mangle Datalog

Source: <https://github.com/google/mangle/blob/main/readthedocs/index.md>

This snippet defines parent-child relationships as facts and a rule to deduce sibling relationships in Mangle Datalog. It demonstrates the declarative nature of the language. No external dependencies are required for this basic example.

```cplint
parent(/oedipus, /antigone).
parent(/oedipus, /ismene).
parent(/oedipus, /eteocles).
parent(/oedipus, /polynices).

sibling(Person1, Person2) ⟸
   parent(P, Person1), parent(P, Person2), Person1 != Person2.
```

--------------------------------

### Building and Testing Mangle Go Library

Source: <https://github.com/google/mangle/blob/main/README.md>

This section provides essential commands for managing the Mangle Go library, including fetching dependencies, building the project, and running tests.

```Shell
go get -t ./...
go build ./...
go test ./...
```

--------------------------------

### Preload Sources with Mangle Interpreter

Source: <https://github.com/google/mangle/blob/main/docs/using_the_interpreter.md>

Preloads a comma-separated list of Mangle sources using the --load flag for atomic analysis.

```bash
go run interpreter/main/main.go --load=<file1>,<file2>
```

--------------------------------

### Shell: Build Mangle documentation with Sphinx

Source: <https://github.com/google/mangle/blob/main/readthedocs/README.md>

This command builds the Mangle documentation into HTML format using Sphinx. It requires an activated virtual environment and specifies the source directory and output directory for the build process. The output is then available in the 'output/html' directory.

```bash
> . manglereadthedocs/bin/activate
(manglereadthedocs) > READTHEDOCS=<path to readthedocs dir>
(manglereadthedocs) > sphinx-build -M html ${READTHEDOCS} output
```

--------------------------------

### Generate Go Parser Sources with ANTLR

Source: <https://github.com/google/mangle/blob/main/README.md>

This snippet shows how to download the ANTLR JAR file, set up an alias for it, and then use ANTLR to generate Go parser sources from a grammar file (Mangle.g4). It specifies the target language as Go, the package name as 'gen', and the output directory.

```shell
wget http://www.antlr.org/download/antlr-4.13.2-complete.jar
alias antlr='java -jar $PWD/antlr-4.13.2-complete.jar'
antlr -Dlanguage=Go -package gen -o ./ parse/gen/Mangle.g4 -visitor
```

--------------------------------

### Basic Mangle Queries

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_api.md>

Demonstrates simple Mangle queries to find volunteers based on availability and skills. These queries assume knowledge of the schema and directly query facts.

```Mangle
?volunteer_time_available(VolunteerID, /tuesday, /afternoon)
?volunteer_skill(VolunteerID, /skill/workshop_facilitation)
```

--------------------------------

### gRPC Service Definition for Volunteer Availability Query

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_api.md>

Defines the gRPC service for querying volunteers based on availability. It includes message formats for requests and replies, and the RPC method signature.

```protobuf
// gRPC service definition
message ByAvRequest { repeated Availability availability = 1; }
message ByAvReply { repeated Volunteer reply = 1; }
service VolunteerQuery {
  rpc GetByMatchingAvailability (ByAvRequest) returns (ByAvReply) {}
}
```

--------------------------------

### Mangle Query with Program for Joins and Unions

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_api.md>

Shows how to construct a Mangle program to handle more complex queries, such as finding volunteers available on multiple time slots or with specific skills. This involves defining intermediate predicates.

```Mangle
program: """
good_time(/tuesday, /afternoon).
good_time(/wednesday, /morning).
matching_availability(VolunteerID, Name, Weekday, Timeslot) :-
    volunteer_time_available(VolunteerID, Weekday, Timeslot),
    good_time(Weekday, Timeslot),
    volunteer_name(VolunteerID, Name).
"""
input: "matching_availability(VolunteerID, Name, Weekday, Timeslot)"
```

--------------------------------

### Datalog Grouping and Aggregation for Project Energy

Source: <https://github.com/google/mangle/blob/main/readthedocs/aggregation.md>

This Datalog code snippet demonstrates how to group project assignments by ProjectID, count the number of software developers, and sum their total hours. It uses a do-transformation with `fn:group_by`, `fn:count`, and `fn:sum` functions.

```Datalog
project_dev_energy(ProjectID, NumDevelopers, TotalHours) ⟸
  project_assignment(ProjectID, VolunteerID, /software_development, Hours)
  |> do fn:group_by(ProjectID),
     NumDevelopers = fn:count(),
     TotalHours = fn:sum(TotalHours).

```

--------------------------------

### Simple Query API

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_api.md>

A basic query interface where input is an atom and output is all matching facts.

```APIDOC
## Simple Query API

### Description
Provides a simple interface for querying a knowledge base. Accepts a single atom as input and returns all matching facts.

### Method
POST

### Endpoint
/query

### Parameters
#### Request Body
- **input** (string) - Required - An atom representing the query.

### Request Example
```json
{
  "input": "?volunteer_time_available(VolunteerID, /tuesday, /afternoon)"
}
```

### Response

#### Success Response (200)

- **output** (array) - A list of facts that match the input query.

#### Response Example

```json
{
  "output": [
    "VolunteerID: volunteer1",
    "VolunteerID: volunteer3"
  ]
}
```

```

--------------------------------

### Flexible Query API with Program

Source: https://github.com/google/mangle/blob/main/docs/example_volunteer_api.md

A generalized query interface that allows an optional Mangle datalog program to be provided alongside the input atom for more complex queries.

```APIDOC
## Flexible Query API with Program

### Description
A more general query interface that accepts an optional Mangle datalog program. This allows for complex queries involving joins, unions, and recursion, enabling retrieval of specific data fields beyond just IDs.

### Method
POST

### Endpoint
/query

### Parameters
#### Request Body
- **program** (string) - Optional - A Mangle datalog program to execute.
- **input** (string) - Required - An atom representing the query, often using predicates defined in the program.

### Request Example
```json
{
  "program": "\"good_time(/tuesday, /afternoon).\n\"good_time(/wednesday, /morning).\nmatching_availability(VolunteerID, Name, Weekday, Timeslot) :-\n    volunteer_time_available(VolunteerID, Weekday, Timeslot),\n    good_time(Weekday, Timeslot),\n    volunteer_name(VolunteerID, Name).\"",
  "input": "matching_availability(VolunteerID, Name, Weekday, Timeslot)"
}
```

### Response

#### Success Response (200)

- **output** (array) - A list of facts matching the complex query specified by the program and input atom.

#### Response Example

```json
{
  "output": [
    "VolunteerID: volunteer1, Name: Alice, Weekday: /tuesday, Timeslot: /afternoon",
    "VolunteerID: volunteer2, Name: Bob, Weekday: /wednesday, Timeslot: /morning"
  ]
}
```

```

--------------------------------

### Query: Teacher-Learner Skill Match (Datalog)

Source: https://github.com/google/mangle/blob/main/docs/example_volunteer_db.md

A Datalog rule to find pairs of volunteers where one has a skill and the other is interested in learning that same skill. It uses the `volunteer_skill` and `volunteer_interest` predicates.

```datalog
teacher_learner_match(Teacher, Learner, Skill) :-
  volunteer_skill(Teacher, Skill),
  volunteer_interest(Learner, Skill).

```

--------------------------------

### Mangle Query with Structured Data

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_api.md>

Illustrates how to use structured data types within Mangle queries for more complex criteria, such as specifying a list of desired time slots. This allows for more flexible and expressive queries.

```Mangle
# We can only evaluate this predicate in the context of a query that
# binds the GoodTimeSlots argument to a value.
matching_availability(VolunteerID, Name, GoodTimeSlots) :-
    volunteer_time_available(VolunteerID, Weekday, Timeslot),
    :list:member(fn:pair(Weekday, Timeslot), GoodTimeSlots),
    volunteer_name(VolunteerID, Name).
```

--------------------------------

### SQL: Translate Zero Developer Case using LEFT JOIN

Source: <https://github.com/google/mangle/blob/main/readthedocs/aggregation.md>

This SQL code translates the Datalog logic for handling projects with zero developers into a concrete implementation. It uses a `LEFT JOIN` between the `project` and `project_assignment` tables to include all projects, even those without assignments. `COALESCE` is used to ensure `TotalHours` is 0 for projects with no assignments.

```SQL
CREATE TABLE project_dev_energy AS
SELECT
  p.ProjectID,
  COUNT(pa.VolunteerID) AS NumDevelopers,
  COALESCE(SUM(pa.Hours), 0) AS TotalHours
FROM
  project AS p
LEFT JOIN
  project_assignment AS pa
ON
  p.ProjectID = pa.ProjectID AND pa.Role = '/software_development'
GROUP BY
  p.ProjectID
```

--------------------------------

### Extensional DB Facts for Volunteers (Datalog)

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_db.md>

Defines volunteer information using extensional predicates. This includes volunteer IDs, names, available times, skills, and interests. These facts represent the base data in the database.

```datalog
# We shall use constant symbols like /v/{number} as identifiers.
volunteer(/v/1).

# Volunteer has a name, some timeslots where they might be available for
# volunteering work, some interests and some skills.
volunteer_name(/v/1, "Aisha Salehi").
volunteer_time_available(/v/1, /monday, /afternoon).
volunteer_time_available(/v/1, /monday, /morning).
volunteer_interest(/v/1, /skill/frontline).
volunteer_skill(/v/1, /skill/admin).
volunteer_skill(/v/1, /skill/facilitate).
volunteer_skill(/v/1, /skill/teaching).

volunteer(/v/2).
volunteer_name(/v/2, "Xin Watson").
volunteer_time_available(/v/2, /monday, /afternoon).
volunteer_interest(/v/2, /skill/facilitate).

```

--------------------------------

### SQL Translation for Project Energy Aggregation

Source: <https://github.com/google/mangle/blob/main/readthedocs/aggregation.md>

This SQL query translates the Datalog `project_dev_energy` rule into standard SQL. It selects ProjectID, counts the number of developers, and sums the hours from the project_assignment table, grouping the results by ProjectID.

```sql
CREATE TABLE project_work_energy AS
SELECT ProjectID, COUNT(VolunteerID) as NumDevelopers, SUM(Hours) as TotalHours
FROM project_assignment
WHERE Role = '/software_developer'
GROUP BY ProjectID

```

--------------------------------

### Execute Query with Mangle Interpreter

Source: <https://github.com/google/mangle/blob/main/docs/using_the_interpreter.md>

Executes a Mangle query using the --exec flag, printing results and exiting with a status code based on the query outcome.

```bash
go run interpreter/main/main.go --exec=<query>
```

--------------------------------

### Mangle Interpreter Commands

Source: <https://github.com/google/mangle/blob/main/docs/using_the_interpreter.md>

Lists the commands available within the Mangle interactive interpreter for managing declarations, clauses, and querying facts.

```plaintext
<decl>.            adds declaration to interactive buffer
<useDecl>.         adds package-use declaration to interactive buffer
<clause>.          adds clause to interactive buffer, evaluates.
?<predicate>       looks up predicate name and queries all facts
?<goal>            queries all facts that match goal
::load <path>      pops interactive buffer and loads source file at <path>
::help             display this help text
::pop              reset state to before interactive defs. or last load command
::show <predicate> shows information about predicate
::show all         shows information about all available predicates
<Ctrl-D>           quit
```

--------------------------------

### Declare Predicate in Mangle Interpreter

Source: <https://github.com/google/mangle/blob/main/docs/using_the_interpreter.md>

Demonstrates declaring a predicate and adding a rule for it in the Mangle interactive interpreter, including querying facts.

```mangle
Decl foo(Arg1, Arg2).
mg >Decl foo(X,Y).
defined [foo(A, B)].
mg >bar(X) :- foo(X, _).
defined [foo(A, B) bar(A)].
mg >foo(1,1).
defined [foo(A, B) bar(A)].
mg >?bar
bar(1)
Found 1 entries for bar.
```

--------------------------------

### Query: Teacher-Learner Session Match (Datalog)

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_db.md>

An enhanced Datalog rule that finds teacher-learner matches considering their availability during the same time slots. It builds upon the `teacher_learner_match` query and adds `volunteer_time_available` checks.

```datalog
teacher_learner_match_session(Teacher, Learner, Skill, PreferredDay, Slot) :-
  teacher_learner_match(Teacher, Learner, Skill),
  volunteer_time_available(Teacher, PreferredDay, Slot),
  volunteer_time_available(Learner, PreferredDay, Slot).

```

--------------------------------

### One or Two Leg Trip Pricing (Mangle)

Source: <https://github.com/google/mangle/blob/main/README.md>

These Mangle rules calculate the price for one or two-leg trips based on direct flight connections. They demonstrate n-ary relations and structured data handling in Mangle.

```Mangle
one_or_two_leg_trip(Codes, Start, Destination, Price) :-
  direct_conn(Code, Start, Destination, Price)
  |> let Codes = [Code].

one_or_two_leg_trip(Codes, Start, Destination, Price) :-
  direct_conn(FirstCode, Start, Connecting, FirstLegPrice).
  direct_conn(SecondCode, Connecting, Destination, SecondLegPrice)
  |> let Code = [FirstCode, SecondCode],
     let Price = fn:plus(FirstLegPrice, SecondLegPrice).
```

--------------------------------

### Query API with List Input

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_api.md>

An enhanced query interface that accepts a list of atoms, returning facts matching at least one atom.

```APIDOC
## Query API with List Input

### Description
An extended query interface that accepts a list of atoms. It returns facts that match at least one of the provided atoms, supporting queries for 'either/or' conditions.

### Method
POST

### Endpoint
/query

### Parameters
#### Request Body
- **input** (array) - Required - A list of atoms representing the queries.

### Request Example
```json
{
  "input": [
    "?volunteer_time_available(VolunteerID, /tuesday, /afternoon)",
    "?volunteer_time_available(VolunteerID, /wednesday, /morning)"
  ]
}
```

### Response

#### Success Response (200)

- **output** (array) - A list of facts that match at least one of the input queries.

#### Response Example

```json
{
  "output": [
    "VolunteerID: volunteer1",
    "VolunteerID: volunteer2"
  ]
}
```

```

--------------------------------

### Counting Projects with Vulnerable Log4j (Mangle)

Source: https://github.com/google/mangle/blob/main/README.md

This Mangle snippet demonstrates aggregation by counting projects identified as having vulnerable log4j versions. It uses piping and aggregation functions for data processing.

```Mangle
count_projects_with_vulnerable_log4j(Num) :-
  projects_with_vulnerable_log4j(P) |> do fn:group_by(), let Num = fn:Count().
```

--------------------------------

### Creating Lists in Mangle

Source: <https://github.com/google/mangle/blob/main/readthedocs/constructedtypes.md>

Shows the syntax for creating lists in Mangle, which are ordered sequences of data items. Lists can be empty or contain multiple elements, with optional trailing commas.

```Mangle
[]
[,]
[0]
[/a, /b, /c]
[/a, /b, /c,]
```

--------------------------------

### Datalog Cartesian Product Rule

Source: <https://github.com/google/mangle/blob/main/docs/spec_explain_relational_algebra.md>

Illustrates the conversion of a cartesian product in relational algebra to a Datalog rule. It defines a predicate 'p' by combining tuples from 'q1' and 'q2' using distinct variables.

```Datalog
p(X..., Y...) :- q1(X...), q2(Y...).
```

--------------------------------

### Datalog Selection Rule

Source: <https://github.com/google/mangle/blob/main/docs/spec_explain_relational_algebra.md>

Demonstrates how to express the selection operation from relational algebra in Datalog. It defines a predicate 'p' from 'q' by adding conditions specified by 'F*'.

```Datalog
p(X...) = q(X...), F*
```

--------------------------------

### Defining Known Skill Constants (Datalog)

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_db.md>

Unary predicates used to list all known skills. These are collected into unary predicates to serve as a vocabulary for skill-related arguments in other predicates, aiding in data consistency.

```datalog
skill(/skill/admin).
skill(/skill/facilitate).
skill(/skill/frontline).
skill(/skill/speaking).
skill(/skill/teaching).
skill(/skill/recruiting).
skill(/skill/workshop_facilitation).

```

--------------------------------

### Mangle Predicate Declaration with Modes

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_api.md>

Demonstrates how to declare Mangle predicates, specifying argument modes (input/output) and visibility. This is a feature for managing predicate interfaces and is noted as not yet implemented.

```Mangle
# GoodTimeSlots must be provided at request time
Decl matching_availability(VolunteerID, Name, GoodTimeSlots)
  descr [mode("+", "+", "-"), public()].

```

--------------------------------

### List Constant Construction in Mangle

Source: <https://github.com/google/mangle/blob/main/docs/structured_data.md>

Shows how to construct list constants in Mangle using fn:list or fn:cons. Lists are finite sequences of values, with equality based on element sequence.

```Mangle
fn:list(value1, ..., valueN)
```

```Mangle
[value1, ..., valueN]
```

```Mangle
fn:cons(value, listValue)
```

--------------------------------

### Spotting Projects with Vulnerable Log4j (SQL)

Source: <https://github.com/google/mangle/blob/main/README.md>

This SQL query performs the same function as the Mangle rule, identifying projects with vulnerable log4j versions. It serves as a comparison to illustrate Mangle's capabilities.

```SQL
SELECT projects.id as P
FROM projects JOIN contains_jar ON projects.id = contains_jar.project_id
WHERE contains_jar.version NOT IN ("2.17.1", "2.12.4", "2.3.2")
```

--------------------------------

### Datalog: Handle Zero Developers in Project Energy Calculation

Source: <https://github.com/google/mangle/blob/main/readthedocs/aggregation.md>

This Datalog rule extends the `project_dev_energy` relation to include projects with zero developers. It uses the previously defined `project_without_developers` relation to identify such projects and sets both `NumDevelopers` and `TotalHours` to 0. This ensures projects with no assigned developers are represented in the final result.

```Datalog
project_dev_energy(ProjectID, NumDevelopers, TotalHours) ←
  project_without_developers(ProjectID),
  NumDevelopers = 0,
  TotalHours = 0.
```

--------------------------------

### Datalog Projection Rule

Source: <https://github.com/google/mangle/blob/main/docs/spec_explain_relational_algebra.md>

Shows how to implement the projection operation in relational algebra using a Datalog rule. It defines a predicate 'p' by selecting specific columns from relation 'q', using '_' for columns not retained.

```Datalog
p(X...) = q(X..., _...).
```

--------------------------------

### Spotting Projects with Vulnerable Log4j (Mangle)

Source: <https://github.com/google/mangle/blob/main/README.md>

This Mangle rule identifies projects containing a vulnerable version of the log4j Java archive. It demonstrates basic Mangle syntax for querying project dependencies and versions.

```Mangle
projects_with_vulnerable_log4j(P) :-
  projects(P),
  contains_jar(P, "log4j", Version),
  Version != "2.17.1",
  Version != "2.12.4",
  Version != "2.3.2".
```

--------------------------------

### Map Constant Construction in Mangle

Source: <https://github.com/google/mangle/blob/main/docs/structured_data.md>

Details the construction of map constants in Mangle using fn:map or bracket notation. Maps associate keys with values, and equality requires identical key-value pairs.

```Mangle
fn:map(key1, value1, ... keyN, valueN)
```

```Mangle
[ key1 : value1, ..., keyN: valueN ]
```

--------------------------------

### Datalog: Identify Projects Without Developers using Negation

Source: <https://github.com/google/mangle/blob/main/readthedocs/aggregation.md>

This Datalog snippet defines a helper relation `project_without_developers` by negating the existence of developers assigned to a project. It first defines `project_with_developers` and then uses negation to find projects where this condition is false. The rule ensures that variables in the head also appear in a non-negated subquery.

```Datalog
project_with_developers(ProjectID) ←
  project_assignment(ProjectID, _, /software_development, _).

project_without_developers(ProjectID) ←
  project_name(ProjectID, _).
  !project_with_developers(ProjectID)
```

--------------------------------

### Mangle Byte String Literals

Source: <https://github.com/google/mangle/blob/main/readthedocs/basictypes.md>

Demonstrates Mangle byte string literals, prefixed with 'b', representing sequences of arbitrary bytes. It shows how to include UTF-8 encoded characters and special byte values.

```Mangle
b"A \x80 byte carries special meaning in UTF8 encoded strings"
b"\x80\x81\x82\n"
b"😤",
```

--------------------------------

### Struct Constant Construction in Mangle

Source: <https://github.com/google/mangle/blob/main/docs/structured_data.md>

Explains the creation of struct constants in Mangle using fn:struct or braced notation. Structs map field names to constants, with equality based on identical fields and values.

```Mangle
fn:struct(key1, value1, ... keyN, valueN)
```

```Mangle
{ key1 : value1, ..., keyN : valueN }
```

--------------------------------

### Creating Structs in Mangle

Source: <https://github.com/google/mangle/blob/main/readthedocs/constructedtypes.md>

Demonstrates the syntax for creating struct data types in Mangle, which are records with named fields. Structs are defined using curly braces, with optional trailing commas.

```Mangle
{}
{,}
{/a: /foo, /b: [/bar, /baz]}
```

--------------------------------

### Tuple Constant Construction in Mangle

Source: <https://github.com/google/mangle/blob/main/docs/structured_data.md>

Illustrates the creation of fixed-length tuple constants using the fn:tuple function symbol in Mangle. Tuples are represented as nested pairs.

```Mangle
fn:tuple(value1, ..., valueN)
```

--------------------------------

### Pair Constant Construction in Mangle

Source: <https://github.com/google/mangle/blob/main/docs/structured_data.md>

Demonstrates the construction of pair constants using the fn:pair function symbol in Mangle. This is a fundamental operation for creating nested data structures.

```Mangle
fn:pair(First, Second)
```

--------------------------------

### Constructing Pairs in Mangle

Source: <https://github.com/google/mangle/blob/main/readthedocs/constructedtypes.md>

Demonstrates the syntax for creating pair data types in Mangle. A pair combines two data items, a left and a right component, defined using fn:pair().

```Mangle
fn:pair("web", 2.0)
fn:pair("hello", fn:pair("world", "!"))
```

--------------------------------

### Recursive Dependency Check (Mangle)

Source: <https://github.com/google/mangle/blob/main/README.md>

These Mangle rules define how to check if a project contains a specific jar, recursively traversing project dependencies. This showcases Mangle's support for recursive queries.

```Mangle
contains_jar(P, Name, Version) :-
  contains_jar_directly(P, Name, Version).

contains_jar(P, Name, Version) :-
  project_depends(P, Q),
  contains_jar(Q, Name, Version).
```

--------------------------------

### Mangle Rules for Data View Conversion

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_db.md>

This snippet demonstrates how to define Mangle rules to convert data from a value table format back to unary relations for IDs and per-property predicates.

```Mangle
volunteer(Id) :-
  volunteer_record(R), :match_field(R, /id, Id).

volunteer_name(Id, Name) :-
  volunteer_record(R), :match_field(R, /id, Id), :match_field(R, /name, Name).
```

--------------------------------

### Constructing Tuples in Mangle

Source: <https://github.com/google/mangle/blob/main/readthedocs/constructedtypes.md>

Illustrates the creation of tuple data types in Mangle. Tuples combine three or more data items, defined using fn:tuple().

```Mangle
fn:tuple("hello", "world", "!")
fn:tuple(1, 2, "three", "4")
```

--------------------------------

### Mangle Value Table Representation

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_db.md>

This snippet shows how to represent structured data using Mangle's value table format, where each record corresponds to a row with a single column of a structured type.

```Mangle
volunteer_record({
   /id:             /v/1,
   /name:           "Aisha Salehi",
   /time_available: [ fn:pair(/monday, /morning), fn:pair(/monday, /afternoon) ],
   /interest:       [ /skill/frontline ],
   /skill:          [ /skill/admin, /skill/facilitate, /skill/teaching ]
}).

volunteer_record({
   /id:             /v/2,
   /name:           "Xin Watson",
   /time_available: [ fn:pair(/monday, /afternoon) ],
   /interest:       [ /skill/frontline ],
   /skill:          [ /skill/admin, /skill/facilitate, /skill/teaching ]
}).
```

--------------------------------

### Define Reachable Paths in a Graph (Datalog)

Source: <https://github.com/google/mangle/blob/main/rust/README.md>

This Datalog rule defines how to find all reachable paths in a graph. It first states that if there's a direct edge from X to Y, then Y is reachable from X. It then recursively defines reachability: if there's an edge from X to Y and Y is reachable from Z, then Z is reachable from X.

```datalog
reachable(X, Y) :- edge(X, Y).
reachable(X, Z) :- edge(X, Y), reachable(Y, Z).
```

--------------------------------

### Mangle Structured Data Syntax

Source: <https://github.com/google/mangle/blob/main/docs/spec_datamodel.md>

Showcases the Mangle syntax for various structured data types, including pairs, tuples, lists, maps, and structs, which are evaluated to constants.

```mangle
fn:pair(<fst>, <snd>)
fn:tuple(<arg1>, ..., <argN>)
[ <elem1>, ... <elemN> ]
[ <key>: <value>, ... <key>: <value> ]
{ <label>: <value>, ... <label>: <value> }
```

--------------------------------

### Datalog Fact with Number and String

Source: <https://github.com/google/mangle/blob/main/docs/spec_datamodel.md>

Demonstrates a Datalog fact that includes both a string literal and a numeric literal as arguments to a predicate.

```datalog
question_answer("what is the meaning of life?", 42).
```

--------------------------------

### Vocabulary Declarations for Volunteer Predicates (Mangle)

Source: <https://github.com/google/mangle/blob/main/docs/example_volunteer_db.md>

Mangle declarations to constrain the constants that can be used within specific argument positions of predicates. This ensures data integrity by restricting values to predefined sets like volunteer IDs and skills.

```mangle
Decl volunteer_interest(Volunteer, Skill)
  bound [ /v, /skill ].

Decl volunteer_skill(Volunteer, Skill)
  bound [ /v, /skill ].

```

--------------------------------

### Mangle Descriptor: Doc

Source: <https://github.com/google/mangle/blob/main/docs/spec_decls.md>

Provides documentation for a predicate using one or more strings.

```Mangle
doc(<string>, ... <string>)
```

--------------------------------

### Datalog Set Difference Rule

Source: <https://github.com/google/mangle/blob/main/docs/spec_explain_relational_algebra.md>

Translates the set difference operation from relational algebra into a Datalog rule. It defines a predicate 'p' that includes tuples from 'q1' but excludes those also present in 'q2'.

```Datalog
p(X...) := q1(X...), !q2(X...).
```

--------------------------------

### Mangle Uses Declaration

Source: <https://github.com/google/mangle/blob/main/docs/spec_decls.md>

Declares that the current source unit can refer to names from a specified package.

```Mangle
Uses <pkg>!
```

--------------------------------

### Mangle Descriptor: Mode

Source: <https://github.com/google/mangle/blob/main/docs/spec_decls.md>

Describes the mode of predicate arguments (input '+', output '-', or both '?').

```Mangle
mode()
```

--------------------------------

### Mangle String Literals and Escape Sequences

Source: <https://github.com/google/mangle/blob/main/readthedocs/basictypes.md>

Illustrates Mangle string literals, which are sequences of Unicode characters in UTF-8 encoding. It shows single, double, and backtick quoted strings, along with various escape sequences for special characters.

```Mangle
"foo"
'foo'
"something 'quoted'"
'something "quoted"'

"something \"quoted\" with escapes."
'A single quote \' surrounded by single quotes'
"A single quote \' surrounded by double quotes"
"A double quote \" surrounded by double quotes"
"A newline \n"
"A tab \t"
"Java class files start with \xca\xfe\xba\xbe"
"The \u{01f624} emoji was originally called 'Face with Look of Triumph'"

`
I write, erase, rewrite

Erase again, and then

A poppy blooms.
`
```

--------------------------------

### Query Siblings of Antigone in Mangle Datalog

Source: <https://github.com/google/mangle/blob/main/readthedocs/index.md>

This snippet shows how to query Mangle Datalog to find all siblings of Antigone. It uses a variable 'X' to represent the unknown sibling and demonstrates the output format for query results. This assumes the preceding facts and rules have been loaded.

```cplint
mg >? sibling(/antigone, X)
sibling(/antigone,/eteocles)
sibling(/antigone,/ismene)
sibling(/antigone,/polynices)
Found 3 entries for sibling(/antigone,_).
```

--------------------------------

### Datalog Union Rule

Source: <https://github.com/google/mangle/blob/main/docs/spec_explain_relational_algebra.md>

Represents the union operation in relational algebra using two Datalog rules. It defines a predicate 'p' that can be derived from either 'q1' or 'q2'.

```Datalog
p(X...) :- q1(X...).
p(X...) :- q2(X...).
```

--------------------------------

### Mangle Package Declaration

Source: <https://github.com/google/mangle/blob/main/docs/spec_decls.md>

Declares the package to which the current source unit belongs.

```Mangle
Package <pkg>!
```

--------------------------------

### Mangle Comparison Predicates

Source: <https://github.com/google/mangle/blob/main/docs/spec_builtin_operations.md>

Compares two expressions for equality, inequality, less than, or less than or equal. These are fundamental operations for conditional logic within Mangle.

```Mangle
Left = Right
Left != Right
Left < Right
Left <= Right
```

--------------------------------

### Mangle List Matching (Cons and Nil)

Source: <https://github.com/google/mangle/blob/main/docs/spec_builtin_operations.md>

Matches a list to either its head and tail (cons) or to check if it's an empty list (nil). Essential for recursive list processing.

```Mangle
:match_cons(List, Head, Tail)
:match_nil(List)
```

--------------------------------

### Mangle Descriptor: Argument Description

Source: <https://github.com/google/mangle/blob/main/docs/spec_decls.md>

Describes the purpose of a specific predicate argument.

```Mangle
arg(<Arg>, <string>)
```

--------------------------------

### Mangle Map Matching (Entry)

Source: <https://github.com/google/mangle/blob/main/docs/spec_builtin_operations.md>

Matches a map to extract a specific key-value pair. This allows for targeted access to map elements.

```Mangle
:match_entry(Map, Key, Value)
```

--------------------------------

### Mangle Rule Grammar Definition

Source: <https://github.com/google/mangle/blob/main/parse/README.md>

This snippet defines the core grammar rules for constructing Mangle rules. It specifies the structure of a rule, including its head and body, and the allowed components such as atoms, literals, formulas, and expressions. It also defines identifiers and constants.

```mangle
rule ::= atom '.'
       | atom  ':-' rhs

rhs ::= litOrFml (',' litOrFml)* '.'
      | litOrFml (',' litOrFml)* '|>' transforms

transforms ::= stmts '.'
             | 'do' apply-expr ',' stmts '.'

stmts ::= stmt (',' stmt)*
stmt ::= 'let' var '=' expr

litOrFml ::= atom | '!' atom | cmp

atom ::= pred-ident '(' exprs? ')'

expr ::= apply-expr
       | constant
       | var

exprs ::= expr (',' expr)*

apply-expr ::= fn-ident '(' exprs? ')'
             | [ exprs? ]

cmp ::= expr op expr

op ::= '=' | '!=' | '<' | '>' 

constant   ::= name, number or string constant
fn-ident   ::= ident (starting with 'fn:')
pred-ident ::= ident (not starting with 'fn:')

```

--------------------------------

### Mangle Descriptor: Deferred

Source: <https://github.com/google/mangle/blob/main/docs/spec_decls.md>

Indicates a deferred predicate which must have all argument positions marked as input-only.

```Mangle
deferred()
```

--------------------------------

### Mangle Descriptor: Merge

Source: <https://github.com/google/mangle/blob/main/docs/spec_decls.md>

Enables predicate removal by specifying a merge predicate for custom lattice operations.

```Mangle
merge(<ArgList>, <pred>)
```

--------------------------------

### Mangle Any Type

Source: <https://github.com/google/mangle/blob/main/readthedocs/typeexpressions.md>

The `/any` type expression in Mangle represents any possible datum. It is a universal type.

```Mangle
/any
```

--------------------------------

### Mangle Bounds Declaration

Source: <https://github.com/google/mangle/blob/main/docs/spec_decls.md>

Declares bounds for a predicate, matching the number of arguments with expressions.

```Mangle
bounds [ <Bound1>, ... <BoundN> ]
```

--------------------------------

### Mangle Predicate Declaration

Source: <https://github.com/google/mangle/blob/main/docs/spec_decls.md>

Declares a predicate with optional descriptor items and a terminating dot.

```Mangle
Decl <predicate name>(<Arg1>, ... <ArgN>)
  descr [<descriptor items>]
  bounds [<bound items>]
.
```
