package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	// NOTE: Only postgresql backend is supported at the moment.
	_ "github.com/lib/pq"
)

// Database contains the database instance and the connection string.
type Database struct {
	db     *sql.DB
	config string
}

// NewDatabase returns the database for the given connection string.
func NewDatabase(config string) (*Database, error) {
	db, err := sql.Open("postgres", config)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	for _, migration := range schema {
		_, err = db.Exec(migration)
		if err != nil {
			return nil, err
		}
	}

	return &Database{
		db:     db,
		config: config,
	}, nil
}

// Close closes the database.
// NOTE: "It is rarely necessary to close a DB."
// See: https://pkg.go.dev/database/sql#Open
func (d *Database) Close() error {
	return d.db.Close()
}

// Config returns the db connection string.
func (d *Database) Config() string {
	return d.config
}

// GetL1TokenByAddress returns the ERC20 Token corresponding to the given
// address on L1.
func (d *Database) GetL1TokenByAddress(address string) (*Token, error) {
	const selectL1TokenStatement = `
	SELECT name, symbol, decimals FROM l1_tokens WHERE address = $1;
	`

	var token *Token
	err := txn(d.db, func(tx *sql.Tx) error {
		row := tx.QueryRow(selectL1TokenStatement, address)
		if row.Err() != nil {
			return row.Err()
		}

		var name string
		var symbol string
		var decimals uint8
		err := row.Scan(&name, &symbol, &decimals)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		token = &Token{
			Name:     name,
			Symbol:   symbol,
			Decimals: decimals,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return token, nil
}

// GetL2TokenByAddress returns the ERC20 Token corresponding to the given
// address on L2.
func (d *Database) GetL2TokenByAddress(address string) (*Token, error) {
	const selectL2TokenStatement = `
	SELECT name, symbol, decimals FROM l2_tokens WHERE address = $1;
	`

	var token *Token
	err := txn(d.db, func(tx *sql.Tx) error {
		row := tx.QueryRow(selectL2TokenStatement, address)
		if row.Err() != nil {
			return row.Err()
		}

		var name string
		var symbol string
		var decimals uint8
		err := row.Scan(&name, &symbol, &decimals)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		token = &Token{
			Name:     name,
			Symbol:   symbol,
			Decimals: decimals,
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return token, nil
}

// AddL1Token inserts the Token details for the given address into the known L1
// tokens database.
// NOTE: a Token MUST have a unique address
func (d *Database) AddL1Token(address string, token *Token) error {
	const insertTokenStatement = `
	INSERT INTO l1_tokens
		(address, name, symbol, decimals)
	VALUES
		($1, $2, $3, $4)
	`

	return txn(d.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(
			insertTokenStatement,
			address,
			token.Name,
			token.Symbol,
			token.Decimals,
		)
		return err
	})
}

// AddL2Token inserts the Token details for the given address into the known L2
// tokens database.
// NOTE: a Token MUST have a unique address
func (d *Database) AddL2Token(address string, token *Token) error {
	const insertTokenStatement = `
	INSERT INTO l2_tokens
		(address, name, symbol, decimals)
	VALUES
		($1, $2, $3, $4)
	`

	return txn(d.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(
			insertTokenStatement,
			address,
			token.Name,
			token.Symbol,
			token.Decimals,
		)
		return err
	})
}

// AddIndexedL1Block inserts the indexed block i.e. the L1 block containing all
// scanned Deposits into the known deposits database.
// NOTE: the block hash MUST be unique
func (d *Database) AddIndexedL1Block(block *IndexedL1Block) error {
	const insertBlockStatement = `
	INSERT INTO l1_blocks
		(hash, parent_hash, number, timestamp)
	VALUES
		($1, $2, $3, $4)
	`

	const insertDepositStatement = `
	INSERT INTO deposits
		(guid, from_address, to_address, l1_token, l2_token, amount, tx_hash, log_index, l1_block_hash, data)
	VALUES
		($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	const insertWithdrawalStatement = `
	INSERT INTO withdrawals
		(guid, from_address, to_address, l1_token, l2_token, amount, tx_hash, log_index, l1_block_hash, data)
	VALUES
		($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	ON CONFLICT (tx_hash)
		DO UPDATE SET l1_block_hash = $9;
	`

	return txn(d.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(
			insertBlockStatement,
			block.Hash.String(),
			block.ParentHash.String(),
			block.Number,
			block.Timestamp,
		)
		if err != nil {
			return err
		}

		if len(block.Deposits) == 0 {
			return nil
		}

		for _, deposit := range block.Deposits {
			_, err = tx.Exec(
				insertDepositStatement,
				NewGUID(),
				deposit.FromAddress.String(),
				deposit.ToAddress.String(),
				deposit.L1Token.String(),
				deposit.L2Token.String(),
				deposit.Amount.String(),
				deposit.TxHash.String(),
				deposit.LogIndex,
				block.Hash.String(),
				deposit.Data,
			)
			if err != nil {
				return err
			}
		}

		if len(block.Withdrawals) == 0 {
			return nil
		}

		for _, withdrawal := range block.Withdrawals {
			_, err = tx.Exec(
				insertWithdrawalStatement,
				NewGUID(),
				withdrawal.FromAddress.String(),
				withdrawal.ToAddress.String(),
				withdrawal.L1Token.String(),
				withdrawal.L2Token.String(),
				withdrawal.Amount.String(),
				withdrawal.TxHash.String(),
				withdrawal.LogIndex,
				block.Hash.String(),
				withdrawal.Data,
			)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// AddIndexedL2Block inserts the indexed block i.e. the L2 block containing all
// scanned Withdrawals into the known withdrawals database.
// NOTE: the block hash MUST be unique
func (d *Database) AddIndexedL2Block(block *IndexedL2Block) error {
	const insertBlockStatement = `
	INSERT INTO l2_blocks
		(hash, parent_hash, number, timestamp)
	VALUES
		($1, $2, $3, $4)
	`

	const insertWithdrawalStatement = `
	INSERT INTO withdrawals
		(guid, from_address, to_address, l1_token, l2_token, amount, tx_hash, log_index, l2_block_hash, data)
	VALUES
		($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	return txn(d.db, func(tx *sql.Tx) error {
		_, err := tx.Exec(
			insertBlockStatement,
			block.Hash.String(),
			block.ParentHash.String(),
			block.Number,
			block.Timestamp,
		)
		if err != nil {
			return err
		}

		if len(block.Withdrawals) == 0 {
			return nil
		}

		for _, withdrawal := range block.Withdrawals {
			_, err = tx.Exec(
				insertWithdrawalStatement,
				NewGUID(),
				withdrawal.FromAddress.String(),
				withdrawal.ToAddress.String(),
				withdrawal.L1Token.String(),
				withdrawal.L2Token.String(),
				withdrawal.Amount.String(),
				withdrawal.TxHash.String(),
				withdrawal.LogIndex,
				block.Hash.String(),
				withdrawal.Data,
			)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// GetDepositsByAddress returns the list of Deposits indexed for the given
// address paginated by the given params.
func (d *Database) GetDepositsByAddress(address common.Address, page PaginationParam) (*PaginatedDeposits, error) {
	const selectDepositsStatement = `
	SELECT
		deposits.guid, deposits.from_address, deposits.to_address,
		deposits.amount, deposits.tx_hash, deposits.data,
		deposits.l1_token, deposits.l2_token,
		l1_tokens.name, l1_tokens.symbol, l1_tokens.decimals,
		l1_blocks.number, l1_blocks.timestamp
	FROM deposits
		INNER JOIN l1_blocks ON deposits.l1_block_hash=l1_blocks.hash
		INNER JOIN l1_tokens ON deposits.l1_token=l1_tokens.address
	WHERE deposits.from_address = $1 ORDER BY l1_blocks.timestamp LIMIT $2 OFFSET $3;
	`
	var deposits []DepositJSON

	err := txn(d.db, func(tx *sql.Tx) error {
		rows, err := tx.Query(selectDepositsStatement, address.String(), page.Limit, page.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var deposit DepositJSON
			var l1Token Token
			if err := rows.Scan(
				&deposit.GUID, &deposit.FromAddress, &deposit.ToAddress,
				&deposit.Amount, &deposit.TxHash, &deposit.Data,
				&l1Token.Address, &deposit.L2Token,
				&l1Token.Name, &l1Token.Symbol, &l1Token.Decimals,
				&deposit.BlockNumber, &deposit.BlockTimestamp,
			); err != nil {
				return err
			}
			deposit.L1Token = &l1Token
			deposits = append(deposits, deposit)
		}

		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	const selectDepositCountStatement = `
	SELECT
		count(*)
	FROM deposits
		INNER JOIN l1_blocks ON deposits.l1_block_hash=l1_blocks.hash
		INNER JOIN l1_tokens ON deposits.l1_token=l1_tokens.address
	WHERE deposits.from_address = $1;
	`

	var count uint64
	err = txn(d.db, func(tx *sql.Tx) error {
		row := tx.QueryRow(selectDepositCountStatement, address.String())
		if err != nil {
			return err
		}

		return row.Scan(&count)
	})
	if err != nil {
		return nil, err
	}

	page.Total = count

	return &PaginatedDeposits{
		&page,
		deposits,
	}, nil
}

// GetWithdrawalStatus returns the finalization status corresponding to the
// given withdrawal transaction hash.
func (d *Database) GetWithdrawalStatus(hash common.Hash) (*WithdrawalJSON, error) {
	const selectWithdrawalStatement = `
	SELECT
	    withdrawals.guid, withdrawals.from_address, withdrawals.to_address,
		withdrawals.amount, withdrawals.tx_hash, withdrawals.data,
		withdrawals.l1_token, withdrawals.l2_token,
		l2_tokens.name, l2_tokens.symbol, l2_tokens.decimals,
		l1_blocks.number, l1_blocks.timestamp,
		l2_blocks.number, l2_blocks.timestamp
	FROM withdrawals
		INNER JOIN l1_blocks ON withdrawals.l1_block_hash=l1_blocks.hash
		INNER JOIN l2_blocks ON withdrawals.l2_block_hash=l2_blocks.hash
		INNER JOIN l2_tokens ON withdrawals.l2_token=l2_tokens.address
	WHERE withdrawals.tx_hash = $1;
	`

	var withdrawal *WithdrawalJSON
	err := txn(d.db, func(tx *sql.Tx) error {
		row := tx.QueryRow(selectWithdrawalStatement, hash.String())
		if row.Err() != nil {
			return row.Err()
		}

		var l2Token Token
		if err := row.Scan(
			&withdrawal.GUID, &withdrawal.FromAddress, &withdrawal.ToAddress,
			&withdrawal.Amount, &withdrawal.TxHash, &withdrawal.Data,
			&withdrawal.L1Token, &l2Token.Address,
			&l2Token.Name, &l2Token.Symbol, &l2Token.Decimals,
			&withdrawal.L1BlockNumber, &withdrawal.L1BlockTimestamp,
			&withdrawal.L2BlockNumber, &withdrawal.L2BlockTimestamp,
		); err != nil {
			return err
		}
		withdrawal.L2Token = &l2Token

		return nil
	})
	if err != nil {
		return nil, err
	}

	return withdrawal, nil
}

// GetWithdrawalsByAddress returns the list of Withdrawals indexed for the given
// address paginated by the given params.
func (d *Database) GetWithdrawalsByAddress(address common.Address, page PaginationParam) (*PaginatedWithdrawals, error) {
	const selectWithdrawalsStatement = `
	SELECT
	    withdrawals.guid, withdrawals.from_address, withdrawals.to_address,
		withdrawals.amount, withdrawals.tx_hash, withdrawals.data,
		withdrawals.l1_token, withdrawals.l2_token,
		l2_tokens.name, l2_tokens.symbol, l2_tokens.decimals,
		l2_blocks.number, l2_blocks.timestamp
	FROM withdrawals
		INNER JOIN l2_blocks ON withdrawals.l2_block_hash=l2_blocks.hash
		INNER JOIN l2_tokens ON withdrawals.l2_token=l2_tokens.address
	WHERE withdrawals.from_address = $1 ORDER BY l2_blocks.timestamp LIMIT $2 OFFSET $3;
	`
	var withdrawals []WithdrawalJSON

	err := txn(d.db, func(tx *sql.Tx) error {
		rows, err := tx.Query(selectWithdrawalsStatement, address.String(), page.Limit, page.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var withdrawal WithdrawalJSON
			var l2Token Token
			if err := rows.Scan(
				&withdrawal.GUID, &withdrawal.FromAddress, &withdrawal.ToAddress,
				&withdrawal.Amount, &withdrawal.TxHash, &withdrawal.Data,
				&withdrawal.L1Token, &l2Token.Address,
				&l2Token.Name, &l2Token.Symbol, &l2Token.Decimals,
				&withdrawal.L2BlockNumber, &withdrawal.L2BlockTimestamp,
			); err != nil {
				return err
			}
			withdrawal.L2Token = &l2Token
			withdrawals = append(withdrawals, withdrawal)
		}

		return rows.Err()
	})

	if err != nil {
		return nil, err
	}

	const selectWithdrawalCountStatement = `
	SELECT
		count(*)
	FROM withdrawals
		INNER JOIN l2_blocks ON withdrawals.l2_block_hash=l2_blocks.hash
		INNER JOIN l2_tokens ON withdrawals.l2_token=l2_tokens.address
	WHERE withdrawals.from_address = $1;
	`

	var count uint64
	err = txn(d.db, func(tx *sql.Tx) error {
		row := tx.QueryRow(selectWithdrawalCountStatement, address.String())
		if err != nil {
			return err
		}

		return row.Scan(&count)
	})
	if err != nil {
		return nil, err
	}

	page.Total = count

	return &PaginatedWithdrawals{
		&page,
		withdrawals,
	}, nil
}

// GetHighestL1Block returns the highest known L1 block.
func (d *Database) GetHighestL1Block() (*BlockLocator, error) {
	const selectHighestBlockStatement = `
	SELECT number, hash FROM l1_blocks ORDER BY number DESC LIMIT 1
	`

	var highestBlock *BlockLocator
	err := txn(d.db, func(tx *sql.Tx) error {
		row := tx.QueryRow(selectHighestBlockStatement)
		if row.Err() != nil {
			return row.Err()
		}

		var number uint64
		var hash string
		err := row.Scan(&number, &hash)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				highestBlock = nil
				return nil
			}
			return err
		}

		highestBlock = &BlockLocator{
			Number: number,
			Hash:   common.HexToHash(hash),
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return highestBlock, nil
}

// GetHighestL2Block returns the highest known L2 block.
func (d *Database) GetHighestL2Block() (*BlockLocator, error) {
	const selectHighestBlockStatement = `
	SELECT number, hash FROM l2_blocks ORDER BY number DESC LIMIT 1
	`

	var highestBlock *BlockLocator
	err := txn(d.db, func(tx *sql.Tx) error {
		row := tx.QueryRow(selectHighestBlockStatement)
		if row.Err() != nil {
			return row.Err()
		}

		var number uint64
		var hash string
		err := row.Scan(&number, &hash)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				highestBlock = nil
				return nil
			}
			return err
		}

		highestBlock = &BlockLocator{
			Number: number,
			Hash:   common.HexToHash(hash),
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return highestBlock, nil
}

// GetIndexedL1BlockByHash returns the L1 block by it's hash.
func (d *Database) GetIndexedL1BlockByHash(hash common.Hash) (*IndexedL1Block, error) {
	const selectBlockByHashStatement = `
	SELECT
		hash, parent_hash, number, timestamp
	FROM l1_blocks
	WHERE hash = $1
	`

	var block *IndexedL1Block
	err := txn(d.db, func(tx *sql.Tx) error {
		row := tx.QueryRow(selectBlockByHashStatement, hash.String())
		if row.Err() != nil {
			return row.Err()
		}

		var hash string
		var parentHash string
		var number uint64
		var timestamp uint64
		err := row.Scan(&hash, &parentHash, &number, &timestamp)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}

		block = &IndexedL1Block{
			Hash:       common.HexToHash(hash),
			ParentHash: common.HexToHash(parentHash),
			Number:     number,
			Timestamp:  timestamp,
			Deposits:   nil,
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return block, nil
}

const getAirdropQuery = `
SELECT
	address, voter_amount, multisig_signer_amount, gitcoin_amount,
	active_bridged_amount, op_user_amount, op_repeat_user_amount,
    bonus_amount, total_amount
FROM airdrops
WHERE address = $1
`

func (d *Database) GetAirdrop(address common.Address) (*Airdrop, error) {
	row := d.db.QueryRow(getAirdropQuery, strings.ToLower(address.String()))
	if row.Err() != nil {
		return nil, fmt.Errorf("error getting airdrop: %v", row.Err())
	}

	airdrop := new(Airdrop)
	err := row.Scan(
		&airdrop.Address,
		&airdrop.VoterAmount,
		&airdrop.MultisigSignerAmount,
		&airdrop.GitcoinAmount,
		&airdrop.ActiveBridgedAmount,
		&airdrop.OpUserAmount,
		&airdrop.OpRepeatUserAmount,
		&airdrop.BonusAmount,
		&airdrop.TotalAmount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error scanning airdrop: %v", err)
	}
	return airdrop, nil
}
